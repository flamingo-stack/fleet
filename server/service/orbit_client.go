package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/fleetdm/fleet/v4/orbit/pkg/constant"
	"github.com/fleetdm/fleet/v4/orbit/pkg/logging"
	"github.com/fleetdm/fleet/v4/orbit/pkg/luks"
	"github.com/fleetdm/fleet/v4/orbit/pkg/platform"
	"github.com/fleetdm/fleet/v4/pkg/retry"
	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/fleetdm/fleet/v4/server/service/contract"
	"github.com/fleetdm/fleet/v4/server/service/openframe"
	"github.com/rs/zerolog/log"
)

// OrbitClient exposes the Orbit API to communicate with the Fleet server.
type OrbitClient struct {
	*baseClient
	nodeKeyFilePath string
	enrollSecret    string
	hostInfo        fleet.OrbitHostInfo

	enrolledMu sync.Mutex
	enrolled   bool

	// nodeKey is the in-memory node key, authoritative while the process is
	// running. The on-disk file is a persistent cache for surviving restarts.
	nodeKeyMu sync.Mutex
	nodeKey   string

	lastRecordedErrMu sync.Mutex
	lastRecordedErr   error

	configCache                 configCache
	onGetConfigErrFns           *OnGetConfigErrFuncs
	lastNetErrOnGetConfigLogged time.Time

	lastIdleConnectionsCleanupMu sync.Mutex
	lastIdleConnectionsCleanup   time.Time

	// TestNodeKey is used for testing only.
	TestNodeKey string

	// Interfaces that will receive updated configs
	ConfigReceivers []fleet.OrbitConfigReceiver
	// How frequently a new config will be fetched
	ReceiverUpdateInterval time.Duration
	// receiverUpdateContext used by ExecuteConfigReceivers to cancel the update loop.
	receiverUpdateContext context.Context
	// receiverUpdateCancelFunc is used to cancel receiverUpdateContext.
	receiverUpdateCancelFunc context.CancelFunc

	// hostIdentityCertPath is the file path to the host identity certificate issued using SCEP.
	//
	// If set then it will be deleted on HTTP 401 errors from Fleet and it will cause ExecuteConfigReceivers
	// to terminate to trigger a restart.
	hostIdentityCertPath string

	// initiatedIdpAuth is a flag indicating whether a window has been opened
	// to the sign-on page for the organization's Identity Provider.
	initiatedIdpAuth bool

	// openSSOWindow is a function that opens a browser window to the SSO URL.
	openSSOWindow func() error

	// openframe mode
	openFrameMode bool
	authManager   *openframe.OpenFrameAuthorizationManager
}

// time-to-live for config cache
const configCacheTTL = 3 * time.Second

type configCache struct {
	mu          sync.Mutex
	lastUpdated time.Time
	config      *fleet.OrbitConfig
	err         error
}

func (oc *OrbitClient) SetOpenSSOWindowFunc(f func() error) {
	oc.openSSOWindow = f
}

func (oc *OrbitClient) request(verb string, path string, params interface{}, resp interface{}) error {
	return oc.requestWithExternal(verb, path, params, resp, false)
}

// requestWithExternal is used to make requests to Fleet or external URLs. If external is true, the pathOrURL
// is used as the full URL to make the request to.
func (oc *OrbitClient) requestWithExternal(verb string, pathOrURL string, params interface{}, resp interface{}, external bool) error {
	var bodyBytes []byte
	var err error
	if params != nil {
		bodyBytes, err = json.Marshal(params)
		if err != nil {
			return fmt.Errorf("making request json marshalling : %w", err)
		}
	}

	oc.closeIdleConnections()

	ctx := context.Background()
	if os.Getenv("FLEETD_TEST_HTTPTRACE") == "1" {
		ctx = httptrace.WithClientTrace(ctx, testStdoutHTTPTracer)
	}

	var request *http.Request
	var fullURL string
	if external {
		fullURL = pathOrURL
		request, err = http.NewRequestWithContext(
			ctx,
			verb,
			pathOrURL,
			nil,
		)
		if err != nil {
			return err
		}
	} else {
		parsedURL, err := url.Parse(pathOrURL)
		if err != nil {
			return fmt.Errorf("parsing URL: %w", err)
		}

		fullURL = oc.url(parsedURL.Path, parsedURL.RawQuery).String()
		request, err = http.NewRequestWithContext(
			ctx,
			verb,
			fullURL,
			bytes.NewBuffer(bodyBytes),
		)
		if err != nil {
			return err
		}
		oc.setClientCapabilitiesHeader(request)
	}

	// if openframe mode |
	if oc.openFrameMode {
		// Add custom header for all requests
		authToken := oc.authManager.GetToken()
		if authToken != "" {
			request.Header.Add("Authorization", "Bearer " + authToken)
		} else {
			log.Debug().Msg("authToken is empty, not adding Authorization header")
		}
	}

	// Log the request details for debugging
	log.Debug().
		Str("verb", verb).
		Str("path", pathOrURL).
		Str("full_url", fullURL).
		Msg("making HTTP request")

	response, err := oc.http.Do(request)
	if err != nil {
		oc.setLastRecordedError(err)
		return fmt.Errorf("%s %s: %w", verb, pathOrURL, err)
	}
	defer response.Body.Close()

	if err := oc.parseResponse(verb, pathOrURL, response, resp); err != nil {
		oc.setLastRecordedError(err)
		return err
	}
	return nil
}

// OnGetConfigErrFuncs defines functions to be executed on GetConfig errors.
type OnGetConfigErrFuncs struct {
	// OnNetErrFunc receives network and 5XX errors on GetConfig requests.
	// These errors are rate limited to once every 5 minutes.
	OnNetErrFunc func(err error)
	// DebugErrFunc receives all errors on GetConfig requests.
	DebugErrFunc func(err error)
}

var (
	netErrInterval                     = 5 * time.Minute
	configRetryOnNetworkError          = 30 * time.Second
	defaultOrbitConfigReceiverInterval = 30 * time.Second
)

// NewOrbitClient creates a new OrbitClient.
//
//   - rootDir is the Orbit's root directory, where the Orbit node key is loaded-from/stored.
//   - addr is the address of the Fleet server.
//   - orbitHostInfo is the host system information used for enrolling to Fleet.
//   - onGetConfigErrFns can be used to handle errors in the GetConfig request.
func NewOrbitClient(
	rootDir string,
	addr string,
	rootCA string,
	insecureSkipVerify bool,
	enrollSecret string,
	fleetClientCert *tls.Certificate,
	orbitHostInfo fleet.OrbitHostInfo,
	onGetConfigErrFns *OnGetConfigErrFuncs,
	httpSignerWrapper func(*http.Client) *http.Client,
	hostIdentityCertPath string,
	openFrameMode bool,
	authManager *openframe.OpenFrameAuthorizationManager,
) (*OrbitClient, error) {
	orbitCapabilities := fleet.GetOrbitClientCapabilities()
	urlPrefix := ""
	if openFrameMode {
		log.Info().Msg("Add tools agent prefix for openframe mode")
		urlPrefix = "/tools/agent/fleetmdm-server"
	} else {
		log.Info().Msg("Add no tools agent prefix for non-openframe mode")
	}
	bc, err := newBaseClient(addr, insecureSkipVerify, rootCA, urlPrefix, fleetClientCert, orbitCapabilities, httpSignerWrapper)
	if err != nil {
		return nil, err
	}

	nodeKeyFilePath := filepath.Join(rootDir, constant.OrbitNodeKeyFileName)
	ctx, cancelFunc := context.WithCancel(context.Background())

	return &OrbitClient{
		nodeKeyFilePath:            nodeKeyFilePath,
		baseClient:                 bc,
		enrollSecret:               enrollSecret,
		hostInfo:                   orbitHostInfo,
		enrolled:                   false,
		onGetConfigErrFns:          onGetConfigErrFns,
		lastIdleConnectionsCleanup: time.Now(),
		ReceiverUpdateInterval:     defaultOrbitConfigReceiverInterval,
		receiverUpdateContext:      ctx,
		receiverUpdateCancelFunc:   cancelFunc,
		hostIdentityCertPath:       hostIdentityCertPath,
		authManager:                authManager,
		openFrameMode:              openFrameMode,
	}, nil
}

// TriggerOrbitRestart triggers a orbit process restart.
func (oc *OrbitClient) TriggerOrbitRestart(reason string) {
	log.Info().Msgf("orbit restart triggered: %s", reason)
	oc.receiverUpdateCancelFunc()
}

// RestartTriggered returns true if any of the config receivers triggered an orbit restart.
func (oc *OrbitClient) RestartTriggered() bool {
	select {
	case <-oc.receiverUpdateContext.Done():
		return true
	default:
		return false
	}
}

// closeIdleConnections attempts to close idle connections from the pool
// every 55 minutes.
//
// Some load balancers (e.g. AWS ELB) have a maximum lifetime for a connection
// (no matter if the connection is active or not) and will forcefully close the
// connection causing errors in the client (e.g. https://github.com/fleetdm/fleet/issues/18783).
// To prevent these errors, we will attempt to cleanup idle connections every 55
// minutes to not let these connection grow too old. (AWS ELB's default value for maximum
// lifetime of a connection is 3600 seconds.)
func (oc *OrbitClient) closeIdleConnections() {
	oc.lastIdleConnectionsCleanupMu.Lock()
	defer oc.lastIdleConnectionsCleanupMu.Unlock()

	if time.Since(oc.lastIdleConnectionsCleanup) < 55*time.Minute {
		return
	}

	oc.lastIdleConnectionsCleanup = time.Now()

	c, ok := oc.baseClient.http.(*http.Client)
	if !ok {
		return
	}
	t, ok := c.Transport.(*http.Transport)
	if !ok {
		return
	}

	t.CloseIdleConnections()
}

func (oc *OrbitClient) RunConfigReceivers() error {
	config, err := oc.GetConfig()
	if err != nil {
		return fmt.Errorf("RunConfigReceivers get config: %w", err)
	}

	var errs []error
	var errMu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(len(oc.ConfigReceivers))

	for _, receiver := range oc.ConfigReceivers {
		receiver := receiver
		go func() {
			defer func() {
				if err := recover(); err != nil {
					errMu.Lock()
					errs = append(errs, fmt.Errorf("panic occured in receiver: %v", err))
					errMu.Unlock()
				}
				wg.Done()
			}()

			err := receiver.Run(config)
			if err != nil {
				errMu.Lock()
				errs = append(errs, err)
				errMu.Unlock()
			}
		}()
	}

	wg.Wait()

	if len(errs) != 0 {
		return errors.Join(errs...)
	}

	return nil
}

func (oc *OrbitClient) RegisterConfigReceiver(cr fleet.OrbitConfigReceiver) {
	oc.ConfigReceivers = append(oc.ConfigReceivers, cr)
}

func (oc *OrbitClient) ExecuteConfigReceivers() error {
	ticker := time.NewTicker(oc.ReceiverUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-oc.receiverUpdateContext.Done():
			return nil
		case <-ticker.C:
			if err := oc.RunConfigReceivers(); err != nil {
				log.Error().Err(err).Msg("running config receivers")
			}
		}
	}
}

func (oc *OrbitClient) InterruptConfigReceivers(err error) {
	oc.receiverUpdateCancelFunc()
}

// GetConfig returns the Orbit config fetched from Fleet server for this instance of OrbitClient.
// Since this method is called in multiple places, we use a cache with configCacheTTL time-to-live
// to reduce traffic to the Fleet server.
// Upon network errors, this method will retry the get config request (every 30 seconds).
func (oc *OrbitClient) GetConfig() (*fleet.OrbitConfig, error) {
	oc.configCache.mu.Lock()
	defer oc.configCache.mu.Unlock()

	// If time-to-live passed, we update the config cache
	now := time.Now()
	if now.After(oc.configCache.lastUpdated.Add(configCacheTTL)) {
		verb, path := "POST", "/api/fleet/orbit/config"
		var (
			resp fleet.OrbitConfig
			err  error
		)
		// Retry until we don't get a network error or a 5XX error.
		_ = retry.Do(func() error {
			err = oc.authenticatedRequest(verb, path, &orbitGetConfigRequest{}, &resp)
			var (
				netErr        net.Error
				statusCodeErr *statusCodeErr
			)
			if err != nil && oc.onGetConfigErrFns != nil && oc.onGetConfigErrFns.DebugErrFunc != nil {
				oc.onGetConfigErrFns.DebugErrFunc(err)
			}
			if errors.As(err, &netErr) || (errors.As(err, &statusCodeErr) && statusCodeErr.code >= 500) {
				now := time.Now()
				if oc.onGetConfigErrFns != nil && oc.onGetConfigErrFns.OnNetErrFunc != nil && now.After(oc.lastNetErrOnGetConfigLogged.Add(netErrInterval)) {
					oc.onGetConfigErrFns.OnNetErrFunc(err)
					oc.lastNetErrOnGetConfigLogged = now
				}
				return err // retry on network or server 5XX errors
			}
			return nil
		}, retry.WithInterval(configRetryOnNetworkError))
		oc.configCache.config = &resp
		oc.configCache.err = err
		oc.configCache.lastUpdated = now
	}
	return oc.configCache.config, oc.configCache.err
}

// SetOrUpdateDeviceToken sends a request to the server to set or update the
// device token with the given value.
func (oc *OrbitClient) SetOrUpdateDeviceToken(deviceAuthToken string) error {
	verb, path := "POST", "/api/fleet/orbit/device_token"
	params := setOrUpdateDeviceTokenRequest{
		DeviceAuthToken: deviceAuthToken,
	}
	var resp setOrUpdateDeviceTokenResponse
	if err := oc.authenticatedRequest(verb, path, &params, &resp); err != nil {
		return err
	}
	return nil
}

// SetOrUpdateDeviceMappingEmail sends a request to the server to set or update the
// device mapping email with the given value.
func (oc *OrbitClient) SetOrUpdateDeviceMappingEmail(email string) error {
	verb, path := "PUT", "/api/fleet/orbit/device_mapping"
	params := orbitPutDeviceMappingRequest{
		Email: email,
	}
	var resp orbitPutDeviceMappingResponse
	if err := oc.authenticatedRequest(verb, path, &params, &resp); err != nil {
		return err
	}
	return nil
}

// GetHostScript returns the script fetched from Fleet server to run on this
// host.
func (oc *OrbitClient) GetHostScript(execID string) (*fleet.HostScriptResult, error) {
	verb, path := "POST", "/api/fleet/orbit/scripts/request"
	var resp orbitGetScriptResponse
	if err := oc.authenticatedRequest(verb, path, &orbitGetScriptRequest{
		ExecutionID: execID,
	}, &resp); err != nil {
		return nil, err
	}
	return resp.HostScriptResult, nil
}

// SaveHostScriptResult saves the result of running the script on this host.
func (oc *OrbitClient) SaveHostScriptResult(result *fleet.HostScriptResultPayload) error {
	verb, path := "POST", "/api/fleet/orbit/scripts/result"
	var resp orbitPostScriptResultResponse
	if err := oc.authenticatedRequest(verb, path, &orbitPostScriptResultRequest{
		HostScriptResultPayload: result,
	}, &resp); err != nil {
		return err
	}
	return nil
}

func (oc *OrbitClient) GetInstallerDetails(installId string) (*fleet.SoftwareInstallDetails, error) {
	verb, path := "POST", "/api/fleet/orbit/software_install/details"
	var resp orbitGetSoftwareInstallResponse
	if err := oc.authenticatedRequest(verb, path, &orbitGetSoftwareInstallRequest{
		InstallUUID: installId,
	}, &resp); err != nil {
		return nil, err
	}
	return resp.SoftwareInstallDetails, nil
}

func (oc *OrbitClient) SaveInstallerResult(payload *fleet.HostSoftwareInstallResultPayload) error {
	verb, path := "POST", "/api/fleet/orbit/software_install/result"
	var resp orbitPostSoftwareInstallResultResponse
	if err := oc.authenticatedRequest(verb, path, &orbitPostSoftwareInstallResultRequest{
		HostSoftwareInstallResultPayload: payload,
	}, &resp); err != nil {
		return err
	}
	return nil
}

func (oc *OrbitClient) DownloadSoftwareInstaller(installerID uint, downloadDirectory string, progressFunc func(n int)) (string, error) {
	verb, path := "POST", "/api/fleet/orbit/software_install/package?alt=media"
	resp := FileResponse{
		DestPath:     downloadDirectory,
		ProgressFunc: progressFunc,
	}
	if err := oc.authenticatedRequest(verb, path, &orbitDownloadSoftwareInstallerRequest{
		InstallerID: installerID,
	}, &resp); err != nil {
		return "", err
	}
	return resp.GetFilePath(), nil
}

func (oc *OrbitClient) DownloadSoftwareInstallerFromURL(url string, filename string, downloadDirectory string, progressFunc func(int)) (string, error) {
	resp := FileResponse{
		DestPath:      downloadDirectory,
		DestFile:      filename,
		SkipMediaType: true,
		ProgressFunc:  progressFunc,
	}
	if err := oc.requestWithExternal("GET", url, nil, &resp, true); err != nil {
		return "", err
	}
	return resp.GetFilePath(), nil
}

type NullFileResponse struct{}

func (f *NullFileResponse) Handle(resp *http.Response) error {
	_, _, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
	if err != nil {
		return fmt.Errorf("parsing media type from response header: %w", err)
	}
	_, err = io.Copy(io.Discard, resp.Body)
	if err != nil {
		return fmt.Errorf("copying from http stream to io.Discard: %w", err)
	}
	return nil
}

// DownloadAndDiscardSoftwareInstaller downloads the software installer and discards it.
// This method is used during load testing by osquery-perf.
func (oc *OrbitClient) DownloadAndDiscardSoftwareInstaller(installerID uint) error {
	verb, path := "POST", "/api/fleet/orbit/software_install/package?alt=media"
	resp := NullFileResponse{}
	return oc.authenticatedRequest(verb, path, &orbitDownloadSoftwareInstallerRequest{
		InstallerID: installerID,
	}, &resp)
}

// Ping sends a ping request to the orbit/ping endpoint.
func (oc *OrbitClient) Ping() error {
	verb, path := "HEAD", "/api/fleet/orbit/ping"
	err := oc.request(verb, path, nil, nil)
	if err == nil || errors.Is(err, notFoundErr{}) {
		// notFound is ok, it means an old server without the capabilities header
		return nil
	}
	return err
}

func (oc *OrbitClient) enroll() (string, error) {
	verb, path := "POST", "/api/fleet/orbit/enroll"
	params := contract.EnrollOrbitRequest{
		EnrollSecret:      oc.enrollSecret,
		HardwareUUID:      oc.hostInfo.HardwareUUID,
		HardwareSerial:    oc.hostInfo.HardwareSerial,
		Hostname:          oc.hostInfo.Hostname,
		Platform:          oc.hostInfo.Platform,
		PlatformLike:      oc.hostInfo.PlatformLike,
		OsqueryIdentifier: oc.hostInfo.OsqueryIdentifier,
		ComputerName:      oc.hostInfo.ComputerName,
		HardwareModel:     oc.hostInfo.HardwareModel,
	}
	log.Info().
		Str("hardware_uuid", oc.hostInfo.HardwareUUID).
		Str("hostname", oc.hostInfo.Hostname).
		Str("platform", oc.hostInfo.Platform).
		Msg("Enrolling orbit host with Fleet server")

	var resp EnrollOrbitResponse
	err := oc.request(verb, path, params, &resp)
	if err != nil {
		log.Error().Err(err).
			Str("hardware_uuid", oc.hostInfo.HardwareUUID).
			Msg("Orbit enrollment request failed")
		return "", err
	}

	if resp.OrbitNodeKey == "" {
		log.Error().
			Str("hardware_uuid", oc.hostInfo.HardwareUUID).
			Msg("Orbit enrollment returned empty node key")
	} else {
		log.Info().
			Str("hardware_uuid", oc.hostInfo.HardwareUUID).
			Msg("Orbit enrollment succeeded, received node key")
	}

	return resp.OrbitNodeKey, nil
}

// enrollLock helps protect the enrolling process in case mutliple OrbitClients
// want to re-enroll at the same time.
var enrollLock sync.Mutex

// getNodeKeyOrEnroll returns the orbit node key. It checks in-memory cache,
// then the on-disk file, and enrolls with the server as a last resort.
func (oc *OrbitClient) getNodeKeyOrEnroll() (string, error) {
	if oc.TestNodeKey != "" {
		return oc.TestNodeKey, nil
	}

	if cached := oc.getNodeKey(); cached != "" {
		return cached, nil
	}

	enrollLock.Lock()
	defer enrollLock.Unlock()

	if cached := oc.getNodeKey(); cached != "" {
		return cached, nil
	}

	orbitNodeKey, err := os.ReadFile(oc.nodeKeyFilePath)
	switch {
	case err == nil:
		log.Info().Str("node_key_file", oc.nodeKeyFilePath).Msg("orbit node key loaded from file")
		oc.setNodeKey(string(orbitNodeKey))
		return string(orbitNodeKey), nil
	case errors.Is(err, fs.ErrNotExist):
		log.Info().Str("node_key_file", oc.nodeKeyFilePath).Msg("no orbit node key file found, proceeding to enroll")
	default:
		return "", fmt.Errorf("read orbit node key file: %w", err)
	}
	var orbitNodeKey_ string
	if err := retry.Do(
		func() error {
			orbitNodeKey_, err = oc.enrollAndWriteNodeKeyFile()
			return err
		},
		// The below configuration means the following retry intervals (exponential backoff):
		// 10s, 20s, 40s, 80s, 160s and then return the failure (max attempts = 6)
		// thus executing no more than ~6 enroll request failures every ~5 minutes.
		retry.WithInterval(orbitEnrollRetryInterval()),
		retry.WithMaxAttempts(constant.OrbitEnrollMaxRetries),
		retry.WithBackoffMultiplier(constant.OrbitEnrollBackoffMultiplier),
		retry.WithErrorFilter(func(err error) (errorOutcome retry.ErrorOutcome) {
			log.Info().Err(err).Msg("orbit enroll attempt failed")
			switch {
			case errors.Is(err, notFoundErr{}):
				// Do not retry if the endpoint does not exist.
				return retry.ErrorOutcomeDoNotRetry
			case errors.Is(err, ErrEndUserAuthRequired):
				// If we get an ErrEndUserAuthRequired error, then the user
				// needs to authenticate with the identity provider.
				//
				// Open a browser window to the sign-on page and
				// then keep retrying until they authenticate.
				log.Debug().Msg("enroll unauthenticated, waiting for end-user to authenticate via SSO")
				if !oc.initiatedIdpAuth {
					if oc.openSSOWindow == nil {
						log.Error().Msg("SSO window open function not set")
						return retry.ErrorOutcomeNormalRetry
					}
					log.Debug().Msg("opening SSO window")
					openWindowErr := oc.openSSOWindow()
					if openWindowErr != nil {
						log.Error().Err(openWindowErr).Msg("opening SSO window")
						return retry.ErrorOutcomeNormalRetry
					}
					oc.initiatedIdpAuth = true
				}
				// Sleep for 20 seconds, making the total retry interval 30 seconds
				time.Sleep(20 * time.Second)
				return retry.ErrorOutcomeResetAttempts
			default:
				logging.LogErrIfEnvNotSet(constant.SilenceEnrollLogErrorEnvVar, err, "enroll failed, retrying")
				return retry.ErrorOutcomeNormalRetry
			}
		}),
	); err != nil {
		if errors.Is(err, notFoundErr{}) {
			return "", errors.New("enroll endpoint does not exist")
		}
		return "", fmt.Errorf("orbit node key enroll failed, attempts=%d", constant.OrbitEnrollMaxRetries)
	}
	oc.setNodeKey(orbitNodeKey_)
	return orbitNodeKey_, nil
}

// GetNodeKey gets the orbit node key from file.
func (oc *OrbitClient) GetNodeKey() (string, error) {
	orbitNodeKey, err := os.ReadFile(oc.nodeKeyFilePath)
	if err != nil {
		return "", err
	}
	return string(orbitNodeKey), nil
}

func (oc *OrbitClient) enrollAndWriteNodeKeyFile() (string, error) {
	orbitNodeKey, err := oc.enroll()
	if err != nil {
		return "", fmt.Errorf("enroll request: %w", err)
	}

	// Persist to disk; non-fatal since the in-memory cache keeps the key
	// available while the process is alive.
	if err := oc.persistNodeKeyFile(orbitNodeKey); err != nil {
		log.Error().Err(err).Str("node_key_file", oc.nodeKeyFilePath).
			Msg("failed to persist node key to disk, cached in memory until restart")
		oc.logNodeKeyFileDiagnostics()
	}

	return orbitNodeKey, nil
}

// persistNodeKeyFile writes the node key to disk. On Windows, if a direct
// write fails it falls back to renaming the locked file and creating a new one.
func (oc *OrbitClient) persistNodeKeyFile(nodeKey string) error {
	if runtime.GOOS == "windows" {
		return oc.writeNodeKeyFileWindows(nodeKey)
	}
	if err := os.WriteFile(oc.nodeKeyFilePath, []byte(nodeKey), constant.DefaultFileMode); err != nil {
		return fmt.Errorf("write orbit node key file: %w", err)
	}
	log.Info().Str("node_key_file", oc.nodeKeyFilePath).Msg("orbit node key written to file")
	return nil
}

func (oc *OrbitClient) writeNodeKeyFileWindows(nodeKey string) error {
	writeNew := func() error {
		if err := os.WriteFile(oc.nodeKeyFilePath, nil, constant.DefaultFileMode); err != nil {
			return fmt.Errorf("create orbit node key file: %w", err)
		}
		if err := platform.ChmodRestrictFile(oc.nodeKeyFilePath); err != nil {
			return fmt.Errorf("apply ACLs to orbit node key file: %w", err)
		}
		if err := os.WriteFile(oc.nodeKeyFilePath, []byte(nodeKey), constant.DefaultFileMode); err != nil {
			return fmt.Errorf("write orbit node key file: %w", err)
		}
		return nil
	}

	if err := os.WriteFile(oc.nodeKeyFilePath, []byte(nodeKey), constant.DefaultFileMode); err == nil {
		log.Info().Str("node_key_file", oc.nodeKeyFilePath).Msg("orbit node key written to file")
		return nil
	}

	// Rename the locked file out of the way and create a fresh one.
	// MoveFile often succeeds on Windows when WriteFile/DeleteFile cannot.
	stalePath := oc.nodeKeyFilePath + ".stale"
	if err := os.Rename(oc.nodeKeyFilePath, stalePath); err != nil {
		return fmt.Errorf("cannot overwrite or rename locked node key file: %w", err)
	}
	log.Info().Str("stale_path", stalePath).Msg("renamed locked node key file out of the way")

	if err := writeNew(); err != nil {
		if renameBackErr := os.Rename(stalePath, oc.nodeKeyFilePath); renameBackErr != nil {
			log.Warn().Err(renameBackErr).Msg("could not restore renamed node key file")
		}
		return err
	}

	_ = os.Remove(stalePath)
	log.Info().Str("node_key_file", oc.nodeKeyFilePath).Msg("orbit node key written to file after rename fallback")
	return nil
}

func (oc *OrbitClient) logNodeKeyFileDiagnostics() {
	info, err := os.Stat(oc.nodeKeyFilePath)
	if err != nil {
		log.Warn().Err(err).Str("path", oc.nodeKeyFilePath).Msg("node key file stat failed")
	} else {
		log.Warn().
			Str("path", oc.nodeKeyFilePath).
			Int64("size_bytes", info.Size()).
			Str("mode", info.Mode().String()).
			Time("modified", info.ModTime()).
			Msg("stale node key file details")
	}

	dir := filepath.Dir(oc.nodeKeyFilePath)
	dirInfo, err := os.Stat(dir)
	if err != nil {
		log.Warn().Err(err).Str("path", dir).Msg("node key directory stat failed")
	} else {
		log.Warn().
			Str("path", dir).
			Str("mode", dirInfo.Mode().String()).
			Msg("node key directory details")
	}

	probe := filepath.Join(dir, ".orbit-write-probe")
	if err := os.WriteFile(probe, []byte("probe"), constant.DefaultFileMode); err != nil {
		log.Warn().Err(err).Str("path", probe).Msg("directory write probe failed — directory may not be writable")
	} else {
		_ = os.Remove(probe)
	}
}

func (oc *OrbitClient) authenticatedRequest(verb string, path string, params interface{}, resp interface{}) error {
	nodeKey, err := oc.getNodeKeyOrEnroll()
	if err != nil {
		return err
	}

	s := params.(setOrbitNodeKeyer)
	s.setOrbitNodeKey(nodeKey)

	err = oc.request(verb, path, params, resp)
	switch {
	case err == nil:
		oc.setEnrolled(true)
		return nil
	case errors.Is(err, ErrUnauthenticated):
		hasNodeKey := nodeKey != ""
		hasAuthToken := false
		if oc.openFrameMode && oc.authManager != nil {
			hasAuthToken = oc.authManager.GetToken() != ""
		}
		log.Error().
			Str("path", path).
			Bool("has_node_key", hasNodeKey).
			Bool("has_auth_token", hasAuthToken).
			Msg("authenticated request got 401, invalidating node key")

		oc.setNodeKey("")
		oc.setEnrolled(false)

		if err := os.Remove(oc.nodeKeyFilePath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			log.Warn().Err(err).Msg("could not remove node key file, will be overwritten on re-enrollment")
		}

		if oc.hostIdentityCertPath != "" {
			if err := os.Remove(oc.hostIdentityCertPath); err != nil {
				log.Info().Err(err).Msg("remove orbit host identity cert")
			}
			log.Info().Msg("removed orbit host identity cert, triggering a restart")
			oc.receiverUpdateCancelFunc()
		}
		return err
	default:
		return err
	}
}

func (oc *OrbitClient) Enrolled() bool {
	oc.enrolledMu.Lock()
	defer oc.enrolledMu.Unlock()

	return oc.enrolled
}

func (oc *OrbitClient) setEnrolled(v bool) {
	oc.enrolledMu.Lock()
	defer oc.enrolledMu.Unlock()

	oc.enrolled = v
}

func (oc *OrbitClient) getNodeKey() string {
	oc.nodeKeyMu.Lock()
	defer oc.nodeKeyMu.Unlock()

	return oc.nodeKey
}

func (oc *OrbitClient) setNodeKey(v string) {
	oc.nodeKeyMu.Lock()
	defer oc.nodeKeyMu.Unlock()

	oc.nodeKey = v
}

func (oc *OrbitClient) LastRecordedError() error {
	oc.lastRecordedErrMu.Lock()
	defer oc.lastRecordedErrMu.Unlock()

	return oc.lastRecordedErr
}

func (oc *OrbitClient) setLastRecordedError(err error) {
	oc.lastRecordedErrMu.Lock()
	defer oc.lastRecordedErrMu.Unlock()

	oc.lastRecordedErr = fmt.Errorf("%s: %w", time.Now().UTC().Format("2006-01-02T15:04:05Z"), err)
}

func orbitEnrollRetryInterval() time.Duration {
	interval := os.Getenv("FLEETD_ENROLL_RETRY_INTERVAL")
	if interval != "" {
		d, err := time.ParseDuration(interval)
		if err == nil {
			return d
		}
	}
	return constant.OrbitEnrollRetrySleep
}

// SetOrUpdateDiskEncryptionKey sends a request to the server to set or update the disk
// encryption keys and result of the encryption process
func (oc *OrbitClient) SetOrUpdateDiskEncryptionKey(diskEncryptionStatus fleet.OrbitHostDiskEncryptionKeyPayload) error {
	verb, path := "POST", "/api/fleet/orbit/disk_encryption_key"

	var resp orbitPostDiskEncryptionKeyResponse
	if err := oc.authenticatedRequest(verb, path, &orbitPostDiskEncryptionKeyRequest{
		EncryptionKey: diskEncryptionStatus.EncryptionKey,
		ClientError:   diskEncryptionStatus.ClientError,
	}, &resp); err != nil {
		return err
	}
	return nil
}

const httpTraceTimeFormat = "2006-01-02T15:04:05Z"

var testStdoutHTTPTracer = &httptrace.ClientTrace{
	ConnectStart: func(network, addr string) {
		fmt.Printf(
			"httptrace: %s: ConnectStart: %s, %s\n",
			time.Now().UTC().Format(httpTraceTimeFormat), network, addr,
		)
	},
	ConnectDone: func(network, addr string, err error) {
		fmt.Printf(
			"httptrace: %s: ConnectDone: %s, %s, err='%s'\n",
			time.Now().UTC().Format(httpTraceTimeFormat), network, addr, err,
		)
	},
}

// GetSetupExperienceStatus checks the status of the setup experience for this host.
func (oc *OrbitClient) GetSetupExperienceStatus(resetFailedSetupSteps bool) (*fleet.SetupExperienceStatusPayload, error) {
	verb, path := "POST", "/api/fleet/orbit/setup_experience/status"
	var resp getOrbitSetupExperienceStatusResponse
	err := oc.authenticatedRequest(verb, path, &getOrbitSetupExperienceStatusRequest{
		ResetFailedSetupSteps: resetFailedSetupSteps,
	}, &resp)
	if err != nil {
		return nil, err
	}

	return resp.Results, nil
}

func (oc *OrbitClient) SendLinuxKeyEscrowResponse(lr luks.LuksResponse) error {
	verb, path := "POST", "/api/fleet/orbit/luks_data"
	var resp orbitPostLUKSResponse
	if err := oc.authenticatedRequest(verb, path, &orbitPostLUKSRequest{
		Passphrase:  lr.Passphrase,
		KeySlot:     lr.KeySlot,
		Salt:        lr.Salt,
		ClientError: lr.Err,
	}, &resp); err != nil {
		return err
	}

	return nil
}

func (oc *OrbitClient) InitiateSetupExperience() (fleet.SetupExperienceInitResult, error) {
	verb, path := "POST", "/api/fleet/orbit/setup_experience/init"
	var resp orbitSetupExperienceInitResponse
	if err := oc.authenticatedRequest(verb, path, &orbitSetupExperienceInitRequest{}, &resp); err != nil {
		return fleet.SetupExperienceInitResult{}, err
	}

	return resp.Result, nil
}
