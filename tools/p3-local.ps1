#Requires -Version 7.4
<#
Runs actual Jenkins builds using uncommitted local sources. No Git staging, commit or push.
The administrative action applies a reviewed Job DSL definition and approves only its pipeline.
Raw evidence is private under notes/experiments; publish reviewed excerpts in docs/evidence/P3.
#>
param(
    [ValidateSet('Prepare','Run')][string]$Mode='Run',
    [ValidateRange(1,100)][int]$Count=20,
    [ValidatePattern('^([0-9]+)?$')][string]$Seed='',
    [ValidatePattern('^([0-9]+)?$')][string]$ClockMillis='',
    [switch]$NoReruns,
    [string]$LabPath=(Join-Path (Split-Path $PSScriptRoot -Parent) '..\buildlens-lab')
)
$ErrorActionPreference='Stop'
$PSNativeCommandUseErrorActionPreference=$true
$root=Split-Path $PSScriptRoot -Parent
Set-Location $root
$evidence=Join-Path $root 'notes\experiments\P3-local-20261012'
New-Item -ItemType Directory -Force -Path $evidence | Out-Null
function In-Jenkins([string]$script) {
    $script | docker compose exec -T jenkins sh -c "tr -d '\r' | sh -s"
}
if ($Mode -eq 'Prepare') {
    $snapshot=Join-Path $evidence 'snapshot'
    New-Item -ItemType Directory -Force -Path (Join-Path $snapshot 'library') | Out-Null
    New-Item -ItemType Directory -Force -Path (Join-Path $snapshot 'lab') | Out-Null
    $labArchive=Join-Path $evidence 'lab.tar'
    tar.exe -cf $labArchive --exclude=target --exclude=.git -C $LabPath pom.xml README.md Jenkinsfile core api flaky-lab
    $entries=tar.exe -tf $labArchive
    if ($entries | Where-Object {$_ -match '(^|/)(target|\.git)(/|$)'}) { throw 'Snapshot includes build output or Git metadata' }
    tar.exe -xf $labArchive -C (Join-Path $snapshot 'lab')
    foreach($name in @('timedStage','reportBuild','quarantineAwareTests')) {
        $src=Join-Path $root "shared-library\vars\$name.groovy"
        $dest=Join-Path $snapshot "library\$name.groovy"
        # load() needs a returned script object; vars/ itself needs no adapter.
        [IO.File]::WriteAllText($dest,(Get-Content -Raw -LiteralPath $src)+"`nreturn this`n")
    }
    $archive=Join-Path $evidence 'snapshot.tar'
    tar.exe -cf $archive -C $snapshot library lab
    $sha256=(Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    $sha1=(Get-FileHash -LiteralPath $archive -Algorithm SHA1).Hash.ToLowerInvariant()
    $base=(git -C $LabPath rev-parse HEAD).Trim()
    $pipeline=(Get-Content -Raw (Join-Path $PSScriptRoot 'p3-local.pipeline.groovy')).Replace('@SNAPSHOT_SHA256@',$sha256).Replace('@SNAPSHOT_SHA1@',$sha1).Replace('@LAB_BASE_COMMIT@',$base)
    [IO.File]::WriteAllText((Join-Path $evidence 'pipeline.groovy'),$pipeline)
    $manifest=[ordered]@{date=(Get-Date).ToUniversalTime().ToString('o');snapshot_sha256=$sha256;snapshot_sha1=$sha1;lab_base_commit=$base;service_base_commit=(git rev-parse HEAD).Trim();uncommitted=$true;files=@()}
    $sourceFiles=@(Get-ChildItem (Join-Path $root 'cmd'),(Join-Path $root 'internal'),(Join-Path $root 'queries'),(Join-Path $root 'migrations'),(Join-Path $root 'shared-library\vars') -Recurse -File)
    $sourceFiles+=Get-Item (Join-Path $root 'config.yaml'),(Join-Path $root 'go.mod'),(Join-Path $root 'go.sum')
    foreach($file in $sourceFiles) {$manifest.files+=@{path=[IO.Path]::GetRelativePath($root,$file.FullName);sha256=(Get-FileHash $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant()}}
    $manifest | ConvertTo-Json -Depth 5 | Set-Content (Join-Path $evidence 'manifest.json')
    $manifest | ConvertTo-Json -Depth 5 | Set-Content (Join-Path $evidence "manifest-$sha256.json")
    [IO.File]::WriteAllText((Join-Path $evidence "pipeline-$sha256.groovy"),$pipeline)
    docker compose exec -T jenkins-agent mkdir -p /tmp/buildlens-p3-snapshot
    docker cp "$snapshot/." buildlens-jenkins-agent-1:/tmp/buildlens-p3-snapshot/
    docker compose exec -T --user root jenkins-agent chown -R jenkins:jenkins /tmp/buildlens-p3-snapshot
    docker compose exec -T jenkins mkdir -p /tmp/buildlens-p3
    docker cp (Join-Path $evidence 'pipeline.groovy') buildlens-jenkins-1:/tmp/buildlens-p3/pipeline.groovy
    docker cp (Join-Path $PSScriptRoot 'p3-local-bootstrap.groovy') buildlens-jenkins-1:/tmp/buildlens-p3/bootstrap.groovy
    $configured=In-Jenkins @'
set -eu
J=http://localhost:8080
A="$JENKINS_ADMIN_USER:$JENKINS_ADMIN_PASSWORD"
C=$(mktemp)
trap 'rm -f "$C"' EXIT
crumb=$(curl -fsS --max-time 30 -u "$A" -c "$C" -b "$C" "$J/crumbIssuer/api/json" | sed -n 's/.*"crumb":"\([^"]*\)".*/\1/p')
curl -fsS --max-time 60 -u "$A" -c "$C" -b "$C" -H "Jenkins-Crumb: $crumb" --data-urlencode script@/tmp/buildlens-p3/bootstrap.groovy "$J/scriptText"
'@
    if (-not ($configured -match 'P3 local job configured by Job DSL')) {
        throw "Job DSL did not confirm configuration: $($configured | Select-Object -First 3)"
    }
    $configured | Write-Host
    Write-Host "Prepared local snapshot $sha256"
    exit
}
In-Jenkins @'
set -eu
J=http://localhost:8080
A="$JENKINS_ADMIN_USER:$JENKINS_ADMIN_PASSWORD"
C=$(mktemp)
trap 'rm -f "$C"' EXIT
mkdir -p /tmp/buildlens-p3/evidence
crumb=$(curl -fsS --max-time 30 -u "$A" -c "$C" -b "$C" "$J/crumbIssuer/api/json" | sed -n 's/.*"crumb":"\([^"]*\)".*/\1/p')
curl -fsS --max-time 30 -H "Authorization: Bearer $BUILDLENS_INGEST_KEY" 'http://buildlens-server:8080/api/v1/quarantine?repo=nibinrj/buildlens-lab' > /tmp/buildlens-p3/evidence/api-before.json
'@
# One completed build at a time; each has its own deadline, and credentials remain in the controller.
for($index=0;$index -lt $Count;$index++) {
    $buildScript=@'
set -eu
J=http://localhost:8080
A="$JENKINS_ADMIN_USER:$JENKINS_ADMIN_PASSWORD"
P="$J/job/buildlens-p3-local-20261012"
C=$(mktemp)
trap 'rm -f "$C"' EXIT
crumb=$(curl -fsS --max-time 30 -u "$A" -c "$C" -b "$C" "$J/crumbIssuer/api/json" | sed -n 's/.*"crumb":"\([^"]*\)".*/\1/p')
n=$(curl -fsS --max-time 30 -u "$A" "$P/api/json?tree=nextBuildNumber" | sed -n 's/.*"nextBuildNumber":\([0-9]*\).*/\1/p')
curl -fsS --max-time 30 -u "$A" -c "$C" -b "$C" -H "Jenkins-Crumb: $crumb" --data-urlencode 'LAB_SEED=@SEED@' --data-urlencode 'LAB_CLOCK=@CLOCK@' --data-urlencode 'NO_RERUNS=@NO_RERUNS@' "$P/buildWithParameters" -o /dev/null
attempt=0
while :; do
  status=$(curl -s --max-time 30 -u "$A" "$P/$n/api/json?tree=building,result" || true)
  case "$status" in *'"building":false'*) break;; esac
  attempt=$((attempt+1))
  if [ "$attempt" -ge 200 ]; then echo "Build $n timed out" >&2; exit 1; fi
  sleep 3
done
printf '%s\t%s\n' "$n" "$status" >> /tmp/buildlens-p3/evidence/builds.tsv
curl -fsS --max-time 30 -u "$A" "$P/$n/consoleText" > "/tmp/buildlens-p3/evidence/console-$n.txt"
if ! grep -q 'buildlens report: uploaded' "/tmp/buildlens-p3/evidence/console-$n.txt"; then
  echo "Build $n completed without a successful BuildLens upload" >&2; exit 1
fi
curl -fsS --max-time 30 -H "Authorization: Bearer $BUILDLENS_INGEST_KEY" 'http://buildlens-server:8080/api/v1/quarantine?repo=nibinrj/buildlens-lab' > "/tmp/buildlens-p3/evidence/quarantine-after-$n.json"
echo "P3 local build $n: $status"
'@
    In-Jenkins ($buildScript.Replace('@SEED@',$Seed).Replace('@CLOCK@',$ClockMillis).Replace('@NO_RERUNS@',$NoReruns.IsPresent.ToString().ToLowerInvariant()))
}
docker cp buildlens-jenkins-1:/tmp/buildlens-p3/evidence/. "$evidence/"
Write-Host "Actual Jenkins evidence saved to $evidence"
