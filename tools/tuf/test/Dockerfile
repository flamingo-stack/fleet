FROM debian:bookworm-slim@sha256:96e378d7e6531ac9a15ad505478fcc2e69f371b10f5cdf87857c4b8188404716

WORKDIR /usr/src/app

COPY test_tuf file-server ./

CMD ["./file-server", "8081", "repository"]
