#Requires -Version 7.4
$ErrorActionPreference='Stop'
. "$PSScriptRoot/../measure-memory.ps1" -RunFile unused -OutputFile unused
$cases=@(@{input='1GiB';want=1024},@{input='384MiB';want=384},@{input='1024KiB';want=1},@{input='1048576B';want=1},@{input='1MB';want=1000000/1048576},@{input='0B';want=0})
foreach($case in $cases){$actual=Convert-DockerMemory $case.input;if([Math]::Abs($actual-$case.want) -gt 0.000001){throw "Memory conversion failed: $($case.input)"}}
foreach($value in @('-1MiB','NaN','12TB','1MiB extra')){try{Convert-DockerMemory $value | Out-Null;throw 'Invalid memory value accepted'}catch{if($_.Exception.Message -eq 'Invalid memory value accepted'){throw}}}
Write-Host 'Memory conversion: 6 valid units and 4 invalid inputs passed.'
