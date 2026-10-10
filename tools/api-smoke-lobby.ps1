[CmdletBinding()]
param(
    [string]$BaseUrl = 'http://127.0.0.1:8080'
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$script:Pass = 0
$script:Fail = 0

function Assert-True {
    param([bool]$Condition, [string]$Name)
    if ($Condition) {
        $script:Pass++
        Write-Host "  [PASS] $Name" -ForegroundColor Green
    } else {
        $script:Fail++
        Write-Host "  [FAIL] $Name" -ForegroundColor Red
    }
}

function Invoke-Api {
    param(
        [Parameter(Mandatory)][string]$Method,
        [Parameter(Mandatory)][string]$Path,
        [hashtable]$Query = @{},
        $Body = $null,
        [string]$Token = $null
    )
    $url = $BaseUrl.TrimEnd('/') + '/' + $Path.TrimStart('/')
    if ($Query.Count -gt 0) {
        $pairs = foreach ($k in $Query.Keys) {
            '{0}={1}' -f [uri]::EscapeDataString([string]$k), [uri]::EscapeDataString([string]$Query[$k])
        }
        $url += '?' + ($pairs -join '&')
    }
    $headers = @{}
    if ($Token) { $headers['Authorization'] = "Bearer $Token" }
    $params = @{ Method = $Method; Uri = $url; Headers = $headers; UseBasicParsing = $true }
    if ($null -ne $Body) {
        $params['Body'] = ($Body | ConvertTo-Json -Depth 12 -Compress)
        $params['ContentType'] = 'application/json'
    }
    try {
        $resp = Invoke-WebRequest @params
        return @{ Status = [int]$resp.StatusCode; Body = ($resp.Content | ConvertFrom-Json) }
    } catch {
        $status = 0
        $content = $null
        if ($_.Exception.Response) {
            $status = [int]$_.Exception.Response.StatusCode
        }
        if ($_.ErrorDetails -and $_.ErrorDetails.Message) {
            $content = $_.ErrorDetails.Message
        } elseif ($_.Exception.Response) {
            try {
                $stream = $_.Exception.Response.GetResponseStream()
                $reader = New-Object System.IO.StreamReader($stream)
                $content = $reader.ReadToEnd()
                $reader.Close()
            } catch { }
        }
        $parsed = $null
        if ($content) {
            try { $parsed = $content | ConvertFrom-Json } catch { $parsed = $content }
        }
        return @{ Status = $status; Body = $parsed }
    }
}

function New-Hex {
    param([int]$Bytes = 32)
    $needed = $Bytes * 2
    $hex = ''
    while ($hex.Length -lt $needed) { $hex += [guid]::NewGuid().ToString('N') }
    return $hex.Substring(0, $needed)
}

Write-Host ""
Write-Host "z-coc public room lobby smoke test" -ForegroundColor Cyan
Write-Host "Target: $BaseUrl"
Write-Host ""

Write-Host "[health]" -ForegroundColor Yellow
$health = Invoke-Api -Method GET -Path 'health.php'
Assert-True ($health.Status -eq 200 -and $health.Body.status -eq 'ok') 'health.php returns {"status":"ok"}'
if ($health.Status -eq 0) {
    Write-Host "  connection failed - is the server running at $BaseUrl ?" -ForegroundColor Red
    Write-Host ""
    Write-Host ("Result: {0} passed, {1} failed" -f $script:Pass, $script:Fail) -ForegroundColor Red
    exit 1
}

Write-Host "[public room]" -ForegroundColor Yellow
$publicRoomId = 'LOB' + ([guid]::NewGuid().ToString('N').Substring(0, 5).ToUpper())
$publicHostId = 'host' + [guid]::NewGuid().ToString('N').Substring(0, 6)

$createPublic = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'create' } -Body @{ room_id = $publicRoomId; user_id = $publicHostId; is_public = $true }
Assert-True ($createPublic.Status -eq 200 -and $createPublic.Body.status -eq 'success' -and $createPublic.Body.host_token) 'create PUBLIC room returns host_token'

$listPublic = Invoke-Api -Method GET -Path 'room_api.php' -Query @{ action = 'list_public' }
Assert-True ($listPublic.Status -eq 200 -and $listPublic.Body.status -eq 'success') 'list_public succeeds'
$publicEntry = @($listPublic.Body.rooms | Where-Object { $_.room_id -eq $publicRoomId })
Assert-True ($publicEntry.Count -eq 1) 'list_public includes the public room'

$entry = $publicEntry[0]
$memberCountNumeric = $false
if ($null -ne $entry.member_count) {
    $memberCountNumeric = ($entry.member_count -is [int]) -or ($entry.member_count -is [long])
}
Assert-True ($entry.host_id -eq $publicHostId -and $memberCountNumeric) 'public room has host_id and numeric member_count'

Write-Host "[private room]" -ForegroundColor Yellow
$privateRoomId = 'LOB' + ([guid]::NewGuid().ToString('N').Substring(0, 5).ToUpper())
$privateHostId = 'host' + [guid]::NewGuid().ToString('N').Substring(0, 6)

$createPrivate = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'create' } -Body @{ room_id = $privateRoomId; user_id = $privateHostId }
Assert-True ($createPrivate.Status -eq 200 -and $createPrivate.Body.status -eq 'success' -and $createPrivate.Body.host_token) 'create PRIVATE room returns host_token'

$listPublic2 = Invoke-Api -Method GET -Path 'room_api.php' -Query @{ action = 'list_public' }
$privateEntry = @($listPublic2.Body.rooms | Where-Object { $_.room_id -eq $privateRoomId })
Assert-True ($privateEntry.Count -eq 0) 'list_public does NOT include the private room'

Write-Host "[toggle visibility]" -ForegroundColor Yellow
$setPublic = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'set_public' } -Body @{ room_id = $privateRoomId; user_id = $privateHostId; host_token = $createPrivate.Body.host_token; is_public = $true }
Assert-True ($setPublic.Status -eq 200 -and $setPublic.Body.status -eq 'success') 'host set_public with correct token returns 200'

$listPublic3 = Invoke-Api -Method GET -Path 'room_api.php' -Query @{ action = 'list_public' }
$privateEntry2 = @($listPublic3.Body.rooms | Where-Object { $_.room_id -eq $privateRoomId })
Assert-True ($privateEntry2.Count -eq 1) 'list_public now includes the promoted room'

$setPublicBad = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'set_public' } -Body @{ room_id = $privateRoomId; user_id = $privateHostId; host_token = (New-Hex -Bytes 32); is_public = $false }
Assert-True ($setPublicBad.Status -eq 403 -and $setPublicBad.Body.code -eq 'HOST_AUTH_FAILED') 'set_public with WRONG host_token returns 403 HOST_AUTH_FAILED'

Write-Host "[sanity]" -ForegroundColor Yellow
$unknownRoomId = 'LOB' + ([guid]::NewGuid().ToString('N').Substring(0, 5).ToUpper())
$unknownEntry = @($listPublic3.Body.rooms | Where-Object { $_.room_id -eq $unknownRoomId })
Assert-True ($unknownEntry.Count -eq 0) 'list_public does not include an unknown room'

Write-Host ""
$color = if ($script:Fail -eq 0) { 'Green' } else { 'Red' }
Write-Host ("Result: {0} passed, {1} failed" -f $script:Pass, $script:Fail) -ForegroundColor $color
if ($script:Fail -gt 0) { exit 1 }
exit 0
