# Runs the Go checks inside a golang container. The Docker daemon on the
# self-hosted runner rejects Windows bind mounts, so the source is copied in.
# The checks live in ci-go.sh: Windows PowerShell 5.1 strips double quotes from
# inline arguments passed to native commands, which breaks an inline script.
$ErrorActionPreference = 'Continue'
$image = 'golang:1.27.3'
$name = "ddt-ci-go-$($env:GITHUB_RUN_ID)-$PID"
$rc = 1
try {
  docker create --name $name $image bash /src/scripts/ci-go.sh | Out-Null
  if ($LASTEXITCODE -ne 0) { throw 'docker create failed' }
  docker cp "$PWD/." "${name}:/src"
  if ($LASTEXITCODE -ne 0) { throw 'docker cp failed' }
  docker start $name | Out-Null
  $rc = [int](docker wait $name)
  docker logs $name 2>&1 | ForEach-Object { "$_" }
} finally {
  docker rm -f $name | Out-Null
}
exit $rc
