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

Write-Host "[public room: host offline]" -ForegroundColor Yellow
$publicRoomId = 'LOB' + ([guid]::NewGuid().ToString('N').Substring(0, 5).ToUpper())
$publicHostId = 'host' + [guid]::NewGuid().ToString('N').Substring(0, 6)

$createPublic = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'create' } -Body @{ room_id = $publicRoomId; user_id = $publicHostId; is_public = $true }
Assert-True ($createPublic.Status -eq 200 -and $createPublic.Body.status -eq 'success' -and $createPublic.Body.host_token) 'create PUBLIC room returns host_token'
$publicToken = $createPublic.Body.host_token

$listOffline = Invoke-Api -Method GET -Path 'room_api.php' -Query @{ action = 'list_public' }
$offlineEntry = @($listOffline.Body.rooms | Where-Object { $_.room_id -eq $publicRoomId })
Assert-True ($listOffline.Status -eq 200 -and $offlineEntry.Count -eq 0) 'list_public hides the room while the host is offline'

Write-Host "[public room: host online]" -ForegroundColor Yellow
$pullHost = Invoke-Api -Method GET -Path 'room_api.php' -Query @{ action = 'pull'; room_id = $publicRoomId; user_id = $publicHostId }
Assert-True ($pullHost.Status -eq 200 -and $pullHost.Body.status -eq 'success') 'host pull registers presence (200)'

$listOnline = Invoke-Api -Method GET -Path 'room_api.php' -Query @{ action = 'list_public' }
$onlineEntry = @($listOnline.Body.rooms | Where-Object { $_.room_id -eq $publicRoomId })
Assert-True ($onlineEntry.Count -eq 1) 'list_public includes the room once the host is online'
$entry = $onlineEntry[0]
Assert-True ($entry.PSObject.Properties.Name -contains 'module_name') 'lobby entry exposes a module_name field'
Assert-True ($entry.PSObject.Properties.Name -contains 'members') 'lobby entry exposes a members array'
$hostMember = @($entry.members | Where-Object { $_.user_id -eq $publicHostId -and $_.is_host -eq $true })
Assert-True ($hostMember.Count -eq 1 -and $entry.member_count -ge 1) 'members contains the online host (is_host=true) with member_count >= 1'

Write-Host "[module name]" -ForegroundColor Yellow
$pushSync = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'push' } -Body @{ room_id = $publicRoomId; sender_id = $publicHostId; host_token = $publicToken; payload = @{ type = 'sync_module'; moduleName = 'SmokeLobbyModule' } }
Assert-True ($pushSync.Status -eq 200 -and $pushSync.Body.status -eq 'success') 'host push sync_module returns 200'

$listModule = Invoke-Api -Method GET -Path 'room_api.php' -Query @{ action = 'list_public' }
$moduleEntry = @($listModule.Body.rooms | Where-Object { $_.room_id -eq $publicRoomId })
Assert-True ($moduleEntry.Count -eq 1 -and $moduleEntry[0].module_name -eq 'SmokeLobbyModule') 'list_public reports the current module_name'
Assert-True ($moduleEntry[0].PSObject.Properties.Name -contains 'name') 'lobby entry exposes a name field'
Assert-True ($moduleEntry[0].name -eq 'SmokeLobbyModule') 'lobby name defaults to the module name before any rename'

Write-Host "[room name]" -ForegroundColor Yellow
$setName = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'set_name' } -Body @{ room_id = $publicRoomId; user_id = $publicHostId; host_token = $publicToken; name = 'Custom Room X' }
Assert-True ($setName.Status -eq 200 -and $setName.Body.status -eq 'success') 'host set_name with correct host_token returns 200'

$listRenamed = Invoke-Api -Method GET -Path 'room_api.php' -Query @{ action = 'list_public' }
$renamedEntry = @($listRenamed.Body.rooms | Where-Object { $_.room_id -eq $publicRoomId })
Assert-True ($renamedEntry.Count -eq 1 -and $renamedEntry[0].name -eq 'Custom Room X') 'lobby name becomes the custom name after rename'

$clearName = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'set_name' } -Body @{ room_id = $publicRoomId; user_id = $publicHostId; host_token = $publicToken; name = '' }
Assert-True ($clearName.Status -eq 200 -and $clearName.Body.status -eq 'success') 'host set_name with empty name returns 200'

$listReverted = Invoke-Api -Method GET -Path 'room_api.php' -Query @{ action = 'list_public' }
$revertedEntry = @($listReverted.Body.rooms | Where-Object { $_.room_id -eq $publicRoomId })
Assert-True ($revertedEntry.Count -eq 1 -and $revertedEntry[0].name -eq 'SmokeLobbyModule') 'lobby name reverts to the module name after clearing'

$setNameBad = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'set_name' } -Body @{ room_id = $publicRoomId; user_id = $publicHostId; host_token = (New-Hex -Bytes 32); name = 'Nope' }
Assert-True ($setNameBad.Status -eq 403 -and $setNameBad.Body.code -eq 'HOST_AUTH_FAILED') 'set_name with WRONG host_token returns 403 HOST_AUTH_FAILED'

Write-Host "[private room]" -ForegroundColor Yellow
$privateRoomId = 'LOB' + ([guid]::NewGuid().ToString('N').Substring(0, 5).ToUpper())
$privateHostId = 'host' + [guid]::NewGuid().ToString('N').Substring(0, 6)

$createPrivate = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'create' } -Body @{ room_id = $privateRoomId; user_id = $privateHostId }
Assert-True ($createPrivate.Status -eq 200 -and $createPrivate.Body.status -eq 'success' -and $createPrivate.Body.host_token) 'create PRIVATE room returns host_token'
$privateToken = $createPrivate.Body.host_token

$listPrivate = Invoke-Api -Method GET -Path 'room_api.php' -Query @{ action = 'list_public' }
$privateEntry = @($listPrivate.Body.rooms | Where-Object { $_.room_id -eq $privateRoomId })
Assert-True ($privateEntry.Count -eq 0) 'list_public never includes the private room'

Write-Host "[promote private room]" -ForegroundColor Yellow
$pullPrivate = Invoke-Api -Method GET -Path 'room_api.php' -Query @{ action = 'pull'; room_id = $privateRoomId; user_id = $privateHostId }
Assert-True ($pullPrivate.Status -eq 200) 'private host pull registers presence'

$setPublic = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'set_public' } -Body @{ room_id = $privateRoomId; user_id = $privateHostId; host_token = $privateToken; is_public = $true }
Assert-True ($setPublic.Status -eq 200 -and $setPublic.Body.status -eq 'success') 'host set_public with correct host_token returns 200'

$listPromoted = Invoke-Api -Method GET -Path 'room_api.php' -Query @{ action = 'list_public' }
$promotedEntry = @($listPromoted.Body.rooms | Where-Object { $_.room_id -eq $privateRoomId })
Assert-True ($promotedEntry.Count -eq 1) 'list_public now includes the promoted room'

Write-Host "[auth]" -ForegroundColor Yellow
$setPublicBad = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'set_public' } -Body @{ room_id = $privateRoomId; user_id = $privateHostId; host_token = (New-Hex -Bytes 32); is_public = $false }
Assert-True ($setPublicBad.Status -eq 403 -and $setPublicBad.Body.code -eq 'HOST_AUTH_FAILED') 'set_public with WRONG host_token returns 403 HOST_AUTH_FAILED'

Write-Host "[close room]" -ForegroundColor Yellow
$closeRoomId = 'LOB' + ([guid]::NewGuid().ToString('N').Substring(0, 5).ToUpper())
$closeHostId = 'host' + [guid]::NewGuid().ToString('N').Substring(0, 6)

$createClose = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'create' } -Body @{ room_id = $closeRoomId; user_id = $closeHostId; is_public = $true }
Assert-True ($createClose.Status -eq 200 -and $createClose.Body.status -eq 'success' -and $createClose.Body.host_token) 'create room for close returns host_token'
$closeToken = $createClose.Body.host_token

$pullClose = Invoke-Api -Method GET -Path 'room_api.php' -Query @{ action = 'pull'; room_id = $closeRoomId; user_id = $closeHostId }
Assert-True ($pullClose.Status -eq 200 -and $pullClose.Body.status -eq 'success') 'close-room host pull registers presence (200)'

$closeBad = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'close' } -Body @{ room_id = $closeRoomId; user_id = $closeHostId; host_token = (New-Hex -Bytes 32) }
Assert-True ($closeBad.Status -eq 403 -and $closeBad.Body.code -eq 'HOST_AUTH_FAILED') 'close with WRONG host_token returns 403 HOST_AUTH_FAILED'

$closeOk = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'close' } -Body @{ room_id = $closeRoomId; user_id = $closeHostId; host_token = $closeToken }
Assert-True ($closeOk.Status -eq 200 -and $closeOk.Body.status -eq 'success') 'host close with correct host_token returns 200'

$listClosed = Invoke-Api -Method GET -Path 'room_api.php' -Query @{ action = 'list_public' }
$closedEntry = @($listClosed.Body.rooms | Where-Object { $_.room_id -eq $closeRoomId })
Assert-True ($closedEntry.Count -eq 0) 'list_public no longer includes the closed room'

$joinClosed = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'join' } -Body @{ room_id = $closeRoomId }
Assert-True ($joinClosed.Status -eq 404) 'join on a closed room returns 404 (gone)'

$closeAgain = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'close' } -Body @{ room_id = $closeRoomId; user_id = $closeHostId; host_token = $closeToken }
Assert-True ($closeAgain.Status -eq 200 -and $closeAgain.Body.status -eq 'success') 'close again (already closed) returns 200 idempotent'

Write-Host ""
$color = if ($script:Fail -eq 0) { 'Green' } else { 'Red' }
Write-Host ("Result: {0} passed, {1} failed" -f $script:Pass, $script:Fail) -ForegroundColor $color
if ($script:Fail -gt 0) { exit 1 }
exit 0
