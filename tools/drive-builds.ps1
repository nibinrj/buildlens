#Requires -Version 7.4
<# Sequential Jenkins REST driver. Secrets are read from the ignored environment file. #>
[CmdletBinding()]
param(
    [ValidateRange(1,1000)][int]$Count=50,
    [string]$Job='buildlens-lab/main',
    [hashtable]$Parameters=@{},
    [ValidateRange(1,7200)][int]$TimeoutSeconds=900,
    [ValidateRange(1,60)][int]$PollSeconds=3,
    [string]$EnvFile=(Join-Path (Split-Path $PSScriptRoot -Parent) '.env'),
    [string]$EvidenceDirectory='',
    [switch]$AllowMissingUpload,
    [switch]$UsePassword,
    [switch]$MeasureMemory
)

. "$PSScriptRoot/jenkins-driver.ps1"
if($MyInvocation.InvocationName -ne '.') {
    Invoke-BuildDriver -Count $Count -Job $Job -Parameters $Parameters -TimeoutSeconds $TimeoutSeconds -PollSeconds $PollSeconds -EnvFile $EnvFile -EvidenceDirectory $EvidenceDirectory -AllowMissingUpload:$AllowMissingUpload -UsePassword:$UsePassword -MeasureMemory:$MeasureMemory | Out-Null
}
