#Requires -Version 7.4
<#
.SYNOPSIS
    The single entry point for BuildLens tasks (PowerShell 7).

.EXAMPLE
    ./tasks.ps1 up          # build and start the whole stack (postgres, grafana, buildlens-server, jenkins, agent)
    ./tasks.ps1 down        # stop the stack (data volumes are kept)
    ./tasks.ps1 logs        # follow all logs; ./tasks.ps1 logs jenkins for one service
    ./tasks.ps1 test        # go test -race ./... inside Linux, then the shared-library JenkinsPipelineUnit tests
    ./tasks.ps1 test -Quick # go test ./... natively, no race detector, Docker tests included
    ./tasks.ps1 lint        # gofmt, go vet, golangci-lint
    ./tasks.ps1 generate    # sqlc generate (queries/ -> internal/store)
    ./tasks.ps1 jenkins     # show Jenkins and agent status and open http://localhost:8080
    ./tasks.ps1 seed        # re-apply jenkins/casc/jenkins.yaml and jenkins/jobs/seed.groovy without a restart

.NOTES
    Why tests run in a container: -race needs cgo and a C compiler, and this Windows machine has none.
    The pinned golang:1.27.1-trixie image has gcc. The Docker socket is mounted so testcontainers
    can start Postgres next to it (decision D-063 in the private log; docs/study/P1-design.md section 5).
#>
param(
    [Parameter(Position = 0, Mandatory)]
    [ValidateSet('up', 'down', 'logs', 'test', 'lint', 'generate', 'jenkins', 'seed')]
    [string] $Task,

    [Parameter(Position = 1, ValueFromRemainingArguments)]
    [string[]] $Rest,

    [switch] $Quick
)

$ErrorActionPreference = 'Stop'
# Make a failing native command (go, docker, ...) stop the script like a PowerShell error.
$PSNativeCommandUseErrorActionPreference = $true
Set-Location $PSScriptRoot

# Exact toolchain pin: go.mod's toolchain line is only a minimum.
$env:GOTOOLCHAIN = 'go1.27.1'

# Pinned versions (sources: docs/study/P1-design.md section 9).
$GolangciLintVersion = '2.14.0'
$TestImage = 'golang:1.27.1-trixie@sha256:3b77fc618ec235a1ab412de7737f120dd507c57e8d87de4cbb7994fb94275ed5'
$SqlcImage = 'sqlc/sqlc:1.31.1@sha256:70f53171d27b2424e9358869975455a6e955a5aa8e58a998a270a6e34e525537'

function Assert-EnvFile {
    if (-not (Test-Path .env)) {
        throw '.env is missing. Copy .env.example to .env and set real passwords first.'
    }
    $token = Select-String -Path .env -Pattern '^GITHUB_TOKEN=(.+)$' -Quiet
    if (-not $token) {
        Write-Warning 'GITHUB_TOKEN is empty in .env: Jenkins cannot scan GitHub until it is set (decision D-014).'
    }
}

# Runs a shell script inside the Jenkins container, where the admin credentials already are,
# so the password never appears on the host command line. PowerShell ends piped text with a Windows line ending
# (CR LF), and sh would read the CR as part of the last command, so carriage returns are removed first.
function Invoke-InJenkins([string] $Script) {
    $Script | docker compose exec -T jenkins sh -c "tr -d '\r' | sh -s"
}

switch ($Task) {
    'up' {
        Assert-EnvFile
        # --wait blocks until every service with a healthcheck reports healthy.
        docker compose up -d --build --wait
        docker compose ps
    }

    'down' {
        docker compose down
    }

    'logs' {
        docker compose logs --follow --tail 200 @Rest
    }

    'test' {
        if ($Quick) {
            # An explicit DOCKER_HOST skips testcontainers' Docker detection, which failed on Windows when two
            # test binaries started containers at the same moment.
            if (-not $env:DOCKER_HOST) { $env:DOCKER_HOST = 'npipe:////./pipe/docker_engine' }
            go test -count=1 ./...
            break
        }
        $repo = (Resolve-Path .).Path
        docker run --rm `
            -v "${repo}:/src" -w /src `
            -v buildlens-gomod:/go/pkg/mod `
            -v buildlens-gocache:/root/.cache/go-build `
            -v /var/run/docker.sock:/var/run/docker.sock `
            -e TESTCONTAINERS_HOST_OVERRIDE=host.docker.internal `
            -e TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock `
            -e GOTOOLCHAIN=local `
            -e CGO_ENABLED=1 `
            $TestImage `
            go test -race -count=1 ./...

        Write-Host 'shared-library: JenkinsPipelineUnit tests'
        Push-Location shared-library
        try {
            if ($IsWindows) { ./mvnw.cmd -B -ntp test } else { ./mvnw -B -ntp test }
        } finally {
            Pop-Location
        }
    }

    'lint' {
        $unformatted = gofmt -l .
        if ($unformatted) {
            throw "gofmt: these files need formatting:`n$($unformatted -join "`n")"
        }
        Write-Host 'gofmt -l .: no files need formatting'

        go vet ./...
        Write-Host 'go vet ./...: ok'

        $version = golangci-lint version --short
        if ($version -ne $GolangciLintVersion) {
            throw "golangci-lint $GolangciLintVersion expected, found '$version'. Install the pinned release (decision D-011)."
        }
        golangci-lint run ./...
    }

    'jenkins' {
        docker compose ps jenkins jenkins-agent
        Invoke-InJenkins @'
set -eu
curl -fsS -u "$JENKINS_ADMIN_USER:$JENKINS_ADMIN_PASSWORD"   "http://localhost:8080/computer/agent-1/api/json?tree=displayName,offline,offlineCauseReason"
echo
'@
        Write-Host 'Jenkins: http://localhost:8080 (user and password: JENKINS_ADMIN_USER / JENKINS_ADMIN_PASSWORD in .env)'
        Start-Process 'http://localhost:8080'
    }

    'seed' {
        # POST /configuration-as-code/reload re-reads jenkins.yaml, whose "jobs:" entry re-runs seed.groovy.
        # A POST with a password needs a CSRF crumb from the same session, hence the cookie jar.
        Invoke-InJenkins @'
set -eu
J=http://localhost:8080
A="$JENKINS_ADMIN_USER:$JENKINS_ADMIN_PASSWORD"
C=$(mktemp)
trap 'rm -f "$C"' EXIT
crumb=$(curl -fsS -u "$A" -c "$C" -b "$C" "$J/crumbIssuer/api/json" | sed -n 's/.*"crumb":"\([^"]*\)".*/\1/p')
code=$(curl -s -o /dev/null -w '%{http_code}' -u "$A" -c "$C" -b "$C" -H "Jenkins-Crumb: $crumb" -X POST "$J/configuration-as-code/reload")
case "$code" in
  200|302) echo "JCasC reloaded and seed.groovy applied (HTTP $code)" ;;
  *) echo "reload failed: HTTP $code" >&2; exit 1 ;;
esac
'@
    }

    'generate' {
        $repo = (Resolve-Path .).Path
        docker run --rm -v "${repo}:/src" -w /src $SqlcImage generate
        Write-Host 'sqlc generate: internal/store updated'
    }
}
