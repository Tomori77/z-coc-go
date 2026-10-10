[CmdletBinding()]
param(
    [string]$BaseUrl = 'http://127.0.0.1:8080',
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
Write-Host "z-coc friends API smoke test" -ForegroundColor Cyan
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

Write-Host "[setup: two fresh users]" -ForegroundColor Yellow
$emailA = "friend-a+$([guid]::NewGuid().ToString('N').Substring(0,8))@example.com"
$emailB = "friend-b+$([guid]::NewGuid().ToString('N').Substring(0,8))@example.com"

$regA = Invoke-Api -Method POST -Path 'db_api.php' -Query @{ action = 'register' } -Body @{ email = $emailA; password = $Password }
Assert-True ($regA.Status -eq 200 -and $regA.Body.status -eq 'success' -and $regA.Body.user_id -and $regA.Body.auth_token) 'register user A returns user_id + auth_token'
$uuidA = $regA.Body.user_id
$tokenA = $regA.Body.auth_token

$regB = Invoke-Api -Method POST -Path 'db_api.php' -Query @{ action = 'register' } -Body @{ email = $emailB; password = $Password }
Assert-True ($regB.Status -eq 200 -and $regB.Body.status -eq 'success' -and $regB.Body.user_id -and $regB.Body.auth_token) 'register user B returns user_id + auth_token'
$uuidB = $regB.Body.user_id
$tokenB = $regB.Body.auth_token

Write-Host "[search]" -ForegroundColor Yellow
$searchHit = Invoke-Api -Method GET -Path 'friend_api.php' -Query @{ action = 'search'; q = $uuidB } -Token $tokenA
Assert-True ($searchHit.Status -eq 200 -and $searchHit.Body.status -eq 'success' -and $searchHit.Body.user.uuid -eq $uuidB) 'search by exact uuid returns user'

$searchMiss = Invoke-Api -Method GET -Path 'friend_api.php' -Query @{ action = 'search'; q = (New-Hex -Bytes 16) } -Token $tokenA
Assert-True ($searchMiss.Status -eq 404 -and $searchMiss.Body.status -eq 'error') 'search unknown uuid returns 404'

Write-Host "[request + accept]" -ForegroundColor Yellow
$request = Invoke-Api -Method POST -Path 'friend_api.php' -Query @{ action = 'request' } -Token $tokenA -Body @{ target_uuid = $uuidB }
Assert-True ($request.Status -eq 200 -and $request.Body.status -eq 'success') 'A sends friend request to B'

$requestAgain = Invoke-Api -Method POST -Path 'friend_api.php' -Query @{ action = 'request' } -Token $tokenA -Body @{ target_uuid = $uuidB }
Assert-True ($requestAgain.Status -eq 409) 'duplicate pending request returns 409'

$listB = Invoke-Api -Method GET -Path 'friend_api.php' -Query @{ action = 'list' } -Token $tokenB
$pendingB = @($listB.Body.requests | Where-Object { $_.uuid -eq $uuidA })
Assert-True ($listB.Status -eq 200 -and $pendingB.Count -eq 1) 'B list shows incoming request from A'
$requestId = $pendingB[0].id

$accept = Invoke-Api -Method POST -Path 'friend_api.php' -Query @{ action = 'accept' } -Token $tokenB -Body @{ request_id = $requestId }
Assert-True ($accept.Status -eq 200 -and $accept.Body.status -eq 'success') 'B accepts request'

$listA = Invoke-Api -Method GET -Path 'friend_api.php' -Query @{ action = 'list' } -Token $tokenA
$friendB = @($listA.Body.friends | Where-Object { $_.uuid -eq $uuidB })
Assert-True ($listA.Status -eq 200 -and $friendB.Count -eq 1) 'A list shows B as friend'
Assert-True ($friendB.Count -eq 1 -and $null -ne $friendB[0].status) 'friend entry has a status field'

$requestAfterFriends = Invoke-Api -Method POST -Path 'friend_api.php' -Query @{ action = 'request' } -Token $tokenA -Body @{ target_uuid = $uuidB }
Assert-True ($requestAfterFriends.Status -eq 409 -and $requestAfterFriends.Body.status -eq 'error') 'request after becoming friends returns 409'

Write-Host "[invite + respond]" -ForegroundColor Yellow
$roomId = 'FRD' + ([guid]::NewGuid().ToString('N').Substring(0, 5).ToUpper())
$roomCreate = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'create' } -Body @{ room_id = $roomId; user_id = $uuidA }
Assert-True ($roomCreate.Status -eq 200 -and $roomCreate.Body.status -eq 'success' -and $roomCreate.Body.host_token) 'room create returns host_token'

$invite = Invoke-Api -Method POST -Path 'friend_api.php' -Query @{ action = 'invite' } -Token $tokenA -Body @{ friend_uuid = $uuidB; room_id = $roomId }
Assert-True ($invite.Status -eq 200 -and $invite.Body.status -eq 'success') 'A invites B to room'

$listB2 = Invoke-Api -Method GET -Path 'friend_api.php' -Query @{ action = 'list' } -Token $tokenB
$inviteB = @($listB2.Body.invites | Where-Object { $_.room_id -eq $roomId -and $_.uuid -eq $uuidA })
Assert-True ($inviteB.Count -eq 1) 'B list shows invite with room_id'
$inviteId = $inviteB[0].id

$respond = Invoke-Api -Method POST -Path 'friend_api.php' -Query @{ action = 'respond' } -Token $tokenB -Body @{ invite_id = $inviteId; accept = $true }
Assert-True ($respond.Status -eq 200 -and $respond.Body.status -eq 'success') 'B accepts room invite'

Write-Host "[auth]" -ForegroundColor Yellow
$noToken = Invoke-Api -Method GET -Path 'friend_api.php' -Query @{ action = 'list' }
Assert-True ($noToken.Status -eq 401) 'friend_api without token returns 401'

Write-Host ""
$color = if ($script:Fail -eq 0) { 'Green' } else { 'Red' }
Write-Host ("Result: {0} passed, {1} failed" -f $script:Pass, $script:Fail) -ForegroundColor $color
if ($script:Fail -gt 0) { exit 1 }
exit 0
