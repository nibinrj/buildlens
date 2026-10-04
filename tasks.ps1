#Requires -Version 7.4
<#
.SYNOPSIS
    The single entry point for BuildLens tasks (PowerShell 7).

.EXAMPLE
    ./tasks.ps1 up          # build and start postgres, grafana, buildlens-server; wait until healthy
    ./tasks.ps1 down        # stop the stack (data volumes are kept)
    ./tasks.ps1 logs        # follow all logs; ./tasks.ps1 logs buildlens-server for one service
    ./tasks.ps1 test        # go test -race ./... inside Linux (needs Docker; see below)
    ./tasks.ps1 test -Quick # go test ./... natively, no race detector, Docker tests included
    ./tasks.ps1 lint        # gofmt, go vet, golangci-lint
    ./tasks.ps1 generate    # sqlc generate (queries/ -> internal/store)

.NOTES
    Why tests run in a container: -race needs cgo and a C compiler, and this Windows machine has none.
    The pinned golang:1.27.1-trixie image has gcc. The Docker socket is mounted so testcontainers
    can start Postgres next to it (decision D-063 in the private log; docs/study/P1-design.md section 5).
#>
param(
    [Parameter(Position = 0, Mandatory)]
    [ValidateSet('up', 'down', 'logs', 'test', 'lint', 'generate')]
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

    'generate' {
        $repo = (Resolve-Path .).Path
        docker run --rm -v "${repo}:/src" -w /src $SqlcImage generate
        Write-Host 'sqlc generate: internal/store updated'
    }
}
