[CmdletBinding()]
param(
    [string]$BaseUrl = 'http://localhost:8080',
    [string]$Email = '',
    [string]$Password = 'SmokeTest123!'
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
Write-Host "z-coc API smoke test" -ForegroundColor Cyan
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

Write-Host "[auth + cloud sync]" -ForegroundColor Yellow
if ([string]::IsNullOrWhiteSpace($Email)) {
    $Email = "smoke+$([guid]::NewGuid().ToString('N').Substring(0,8))@example.com"
}

$reg = Invoke-Api -Method POST -Path 'db_api.php' -Query @{ action = 'register' } -Body @{ email = $Email; password = $Password }
Assert-True ($reg.Status -eq 200 -and $reg.Body.status -eq 'success' -and $reg.Body.user_id -and $reg.Body.auth_token) 'register returns user_id + auth_token'
$userId = $reg.Body.user_id

$dup = Invoke-Api -Method POST -Path 'db_api.php' -Query @{ action = 'register' } -Body @{ email = $Email; password = $Password }
Assert-True ($dup.Status -eq 409) 'duplicate register returns 409'

$badLogin = Invoke-Api -Method POST -Path 'db_api.php' -Query @{ action = 'login' } -Body @{ email = $Email; password = 'wrong-password' }
Assert-True ($badLogin.Status -eq 401) 'wrong password returns 401'

$login = Invoke-Api -Method POST -Path 'db_api.php' -Query @{ action = 'login' } -Body @{ email = $Email; password = $Password }
Assert-True ($login.Status -eq 200 -and $login.Body.status -eq 'success' -and $login.Body.user_id -eq $userId) 'login returns same user_id'
$token = $login.Body.auth_token

$pullEmpty = Invoke-Api -Method GET -Path 'db_api.php' -Query @{ action = 'pull'; user_id = $userId } -Token $token
Assert-True ($pullEmpty.Status -eq 200 -and $null -eq $pullEmpty.Body) 'pull with no data returns null'

$sync1 = Invoke-Api -Method POST -Path 'db_api.php' -Query @{ action = 'sync' } -Token $token -Body @{ user_id = $userId; settings = @{ theme = 'dark' }; investigators = @(@{ id = 'inv1'; name = 'Smoke' }); saves = @(); tavern_novels = @(); base_revision = 0 }
Assert-True ($sync1.Status -eq 200 -and $sync1.Body.status -eq 'success' -and $sync1.Body.revision -eq 1) 'sync creates revision 1'

$syncConflict = Invoke-Api -Method POST -Path 'db_api.php' -Query @{ action = 'sync' } -Token $token -Body @{ user_id = $userId; settings = @{}; investigators = @(); saves = @(); tavern_novels = @(); base_revision = 0 }
Assert-True ($syncConflict.Status -eq 409 -and $syncConflict.Body.status -eq 'conflict') 'stale base_revision returns 409 conflict'

$pull1 = Invoke-Api -Method GET -Path 'db_api.php' -Query @{ action = 'pull'; user_id = $userId } -Token $token
Assert-True ($pull1.Status -eq 200 -and $pull1.Body.revision -eq 1 -and @($pull1.Body.investigators).Count -eq 1) 'pull returns revision 1 with investigator'

$noAuth = Invoke-Api -Method GET -Path 'db_api.php' -Query @{ action = 'pull'; user_id = $userId }
Assert-True ($noAuth.Status -eq 401) 'pull without token returns 401'

Write-Host "[multiplayer room]" -ForegroundColor Yellow
$roomId = 'SMK' + ([guid]::NewGuid().ToString('N').Substring(0, 5).ToUpper())
$hostId = 'host' + [guid]::NewGuid().ToString('N').Substring(0, 6)
$playerId = 'player' + [guid]::NewGuid().ToString('N').Substring(0, 6)

$roomCreate = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'create' } -Body @{ room_id = $roomId; user_id = $hostId }
Assert-True ($roomCreate.Status -eq 200 -and $roomCreate.Body.status -eq 'success' -and $roomCreate.Body.host_token) 'room create returns host_token'
$hostToken = $roomCreate.Body.host_token

$roomJoin = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'join' } -Body @{ room_id = $roomId }
Assert-True ($roomJoin.Status -eq 200 -and $roomJoin.Body.last_id -eq 0) 'room join returns last_id 0'

$pushHost = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'push' } -Body @{ room_id = $roomId; sender_id = $hostId; host_token = $hostToken; payload = @{ type = 'sync_state'; state = @{ round = 1 } } }
Assert-True ($pushHost.Status -eq 200 -and $pushHost.Body.id -ge 1) 'host push (host-only type) succeeds'

$pushHostNoToken = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'push' } -Body @{ room_id = $roomId; sender_id = $hostId; payload = @{ type = 'sync_state' } }
Assert-True ($pushHostNoToken.Status -eq 403 -and $pushHostNoToken.Body.code -eq 'HOST_AUTH_FAILED') 'host-only push without token returns 403'

$pushPlayer = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'push' } -Body @{ room_id = $roomId; sender_id = $playerId; payload = @{ type = 'chat'; user = @{ id = $playerId }; text = 'hello' } }
Assert-True ($pushPlayer.Status -eq 200 -and $pushPlayer.Body.id -ge 1) 'player push (chat) succeeds'

$roomPull = Invoke-Api -Method GET -Path 'room_api.php' -Query @{ action = 'pull'; room_id = $roomId; since = 0; user_id = $playerId }
Assert-True ($roomPull.Status -eq 200 -and @($roomPull.Body.messages).Count -ge 2) 'room pull returns messages'
Assert-True (@($roomPull.Body.presence | Where-Object { $_.user_id -eq $playerId }).Count -eq 1) 'room pull registers presence'

Write-Host "[library]" -ForegroundColor Yellow
$ownerToken = New-Hex -Bytes 32
$modTitle = 'Smoke Module ' + [guid]::NewGuid().ToString('N').Substring(0, 6)

$libList = Invoke-Api -Method GET -Path 'library_api.php' -Query @{ action = 'list'; section = 'reading' }
Assert-True ($libList.Status -eq 200 -and $libList.Body.status -eq 'success') 'library list reading succeeds'

$libCreate = Invoke-Api -Method POST -Path 'library_api.php' -Query @{ action = 'create' } -Body @{ title = $modTitle; description = 'desc'; content = '# Smoke content'; author_name = 'Smoke'; author_id = $userId; owner_token = $ownerToken; type = 'original'; min_players = 1; max_players = 4 }
Assert-True ($libCreate.Status -eq 200 -and $libCreate.Body.id -ge 1) 'library create returns id'
$moduleId = $libCreate.Body.id

$libDetail = Invoke-Api -Method GET -Path 'library_api.php' -Query @{ action = 'detail'; id = $moduleId }
Assert-True ($libDetail.Status -eq 200 -and $libDetail.Body.module.content -eq '# Smoke content') 'library detail returns content'

$libSearch = Invoke-Api -Method GET -Path 'library_api.php' -Query @{ action = 'list'; section = 'reading'; q = $modTitle }
Assert-True ($libSearch.Status -eq 200 -and @($libSearch.Body.modules | Where-Object { $_.id -eq $moduleId }).Count -eq 1) 'library search finds module'

$libDownload = Invoke-Api -Method POST -Path 'library_api.php' -Query @{ action = 'increment_downloads' } -Body @{ id = $moduleId }
Assert-True ($libDownload.Status -eq 200 -and $libDownload.Body.status -eq 'success') 'library increment_downloads succeeds'

$libDeleteWrong = Invoke-Api -Method POST -Path 'library_api.php' -Query @{ action = 'delete' } -Body @{ id = $moduleId; author_id = $userId; owner_token = (New-Hex -Bytes 32) }
Assert-True ($libDeleteWrong.Status -eq 403) 'library delete with wrong owner token returns 403'

$libDelete = Invoke-Api -Method POST -Path 'library_api.php' -Query @{ action = 'delete' } -Body @{ id = $moduleId; author_id = $userId; owner_token = $ownerToken }
Assert-True ($libDelete.Status -eq 200 -and $libDelete.Body.status -eq 'success') 'library delete succeeds'

$libDetailGone = Invoke-Api -Method GET -Path 'library_api.php' -Query @{ action = 'detail'; id = $moduleId }
Assert-True ($libDetailGone.Status -eq 404) 'deleted module returns 404'

Write-Host "[logout]" -ForegroundColor Yellow
$logout = Invoke-Api -Method POST -Path 'db_api.php' -Query @{ action = 'logout' } -Token $token -Body @{ user_id = $userId }
Assert-True ($logout.Status -eq 200 -and $logout.Body.status -eq 'success') 'logout succeeds'

$pullAfterLogout = Invoke-Api -Method GET -Path 'db_api.php' -Query @{ action = 'pull'; user_id = $userId } -Token $token
Assert-True ($pullAfterLogout.Status -eq 401) 'pull after logout returns 401'

Write-Host ""
$color = if ($script:Fail -eq 0) { 'Green' } else { 'Red' }
Write-Host ("Result: {0} passed, {1} failed" -f $script:Pass, $script:Fail) -ForegroundColor $color
if ($script:Fail -gt 0) { exit 1 }
exit 0
