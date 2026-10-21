#Requires -Version 7.4
$ErrorActionPreference='Stop'
$PSNativeCommandUseErrorActionPreference=$true
$root=Split-Path $PSScriptRoot -Parent
Set-Location $root
. "$PSScriptRoot/jenkins-driver.ps1"
$settings=Read-BuildLensEnv (Join-Path $root '.env')
if($settings.JENKINS_API_TOKEN) { Write-Host 'Jenkins driver token is already configured.'; return }
docker compose cp tools/driver-token.groovy jenkins:/tmp/buildlens-driver-token.groovy
docker compose exec -T --user root jenkins chown jenkins:jenkins /tmp/buildlens-driver-token.groovy
$script=@'
set -eu
J=http://localhost:8080
A="$JENKINS_ADMIN_USER:$JENKINS_ADMIN_PASSWORD"
C=$(mktemp)
trap 'rm -f "$C" /tmp/buildlens-driver-token.groovy' EXIT
crumb=$(curl -fsS --max-time 20 -u "$A" -c "$C" -b "$C" "$J/crumbIssuer/api/json" | sed -n 's/.*"crumb":"\([^"]*\)".*/\1/p')
curl -fsS --max-time 30 -u "$A" -c "$C" -b "$C" -H "Jenkins-Crumb: $crumb" --data-urlencode script@/tmp/buildlens-driver-token.groovy "$J/scriptText"
'@
$token=($script | docker compose exec -T jenkins sh -c "tr -d '\r' | sh -s") -join ''
if($token -notmatch '^[a-f0-9]{34}$') { throw 'Jenkins did not return a valid driver token' }
$envPath=Join-Path $root '.env'
$text=[IO.File]::ReadAllText($envPath)
if($text -match '(?m)^JENKINS_API_TOKEN=.*$') { $text=[regex]::Replace($text,'(?m)^JENKINS_API_TOKEN=.*$',"JENKINS_API_TOKEN=$token") }
else { $text=$text.TrimEnd()+"`nJENKINS_API_TOKEN=$token`n" }
[IO.File]::WriteAllText($envPath,$text)
Write-Host 'Jenkins driver token saved to ignored .env. Token value was not printed.'
