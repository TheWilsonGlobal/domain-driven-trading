# Runs the Go checks inside a golang container. The Docker daemon on the
# self-hosted runner rejects Windows bind mounts, so the source is copied in.
$ErrorActionPreference = 'Stop'
$image = 'golang:1.27.3'
$name = "ddt-ci-go-$($env:GITHUB_RUN_ID)-$PID"
$script = @'
set -e
cd /src
out="$(gofmt -l cmd internal scripts)"
if [ -n "$out" ]; then echo "gofmt needed:"; echo "$out"; exit 1; fi
go vet ./...
CGO_ENABLED=1 go test -race -count=1 ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...
'@
docker create --name $name $image bash -c $script | Out-Null
try {
  docker cp "$PWD/." "${name}:/src"
  docker start -a $name
  if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
} finally {
  docker rm -f $name | Out-Null
}
