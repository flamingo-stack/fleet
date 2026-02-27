package dependency

import (
	"fmt"

	"github.com/fleetdm/fleet/v4/server/archtest/test_files/transative"
)

const Item = "depend on me"

func SomeMethod() {
	fmt.Println(transative.NowYouDependOnMe)
}
