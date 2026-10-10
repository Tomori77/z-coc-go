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
Write-Host "z-coc admin API smoke test" -ForegroundColor Cyan
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

Write-Host "[admin bootstrap]" -ForegroundColor Yellow
$adminEmail = 'smoke-admin@example.com'
$regAdmin = Invoke-Api -Method POST -Path 'db_api.php' -Query @{ action = 'register' } -Body @{ email = $adminEmail; password = $Password }
Assert-True ($regAdmin.Status -eq 200 -or $regAdmin.Status -eq 409) 'admin register returns 200 (new) or 409 (existing)'

$loginAdmin = Invoke-Api -Method POST -Path 'db_api.php' -Query @{ action = 'login' } -Body @{ email = $adminEmail; password = $Password }
Assert-True ($loginAdmin.Status -eq 200 -and $loginAdmin.Body.status -eq 'success' -and $loginAdmin.Body.auth_token) 'admin login succeeds and returns token (triggers promotion)'
$adminToken = $loginAdmin.Body.auth_token
$adminUuid = $loginAdmin.Body.user_id

Write-Host "[overview]" -ForegroundColor Yellow
$overview = Invoke-Api -Method GET -Path 'admin_api.php' -Query @{ action = 'overview' } -Token $adminToken
Assert-True ($overview.Status -eq 200 -and $overview.Body.status -eq 'success' -and $null -ne $overview.Body.stats) 'overview returns 200 with stats'
Assert-True ($overview.Body.stats.users -ge 1) 'overview reports at least one user'

Write-Host "[non-admin access]" -ForegroundColor Yellow
$emailC = "admin-c+$([guid]::NewGuid().ToString('N').Substring(0,8))@example.com"
$regC = Invoke-Api -Method POST -Path 'db_api.php' -Query @{ action = 'register' } -Body @{ email = $emailC; password = $Password }
Assert-True ($regC.Status -eq 200 -and $regC.Body.status -eq 'success' -and $regC.Body.auth_token) 'register normal user C returns token'
$uuidC = $regC.Body.user_id
$tokenC = $regC.Body.auth_token

$forbidden = Invoke-Api -Method GET -Path 'admin_api.php' -Query @{ action = 'overview' } -Token $tokenC
Assert-True ($forbidden.Status -eq 403 -and $forbidden.Body.message -eq 'Forbidden') 'non-admin token gets 403 Forbidden'

$noToken = Invoke-Api -Method GET -Path 'admin_api.php' -Query @{ action = 'overview' }
Assert-True ($noToken.Status -eq 401) 'admin_api without token returns 401'

Write-Host "[users.list + user.detail]" -ForegroundColor Yellow
$list = Invoke-Api -Method GET -Path 'admin_api.php' -Query @{ action = 'users.list'; q = $emailC } -Token $adminToken
$cRow = @($list.Body.users | Where-Object { $_.uuid -eq $uuidC })
Assert-True ($list.Status -eq 200 -and $cRow.Count -eq 1) 'users.list search includes user C'

$detail = Invoke-Api -Method GET -Path 'admin_api.php' -Query @{ action = 'user.detail'; user_id = $uuidC } -Token $adminToken
Assert-True ($detail.Status -eq 200 -and $detail.Body.user.uuid -eq $uuidC) 'user.detail returns user C'
$names = @($detail.Body.user.PSObject.Properties.Name)
Assert-True (-not ($names -contains 'investigators') -and -not ($names -contains 'saves')) 'user.detail omits cloud-data content keys'
Assert-True (($names -contains 'settings_bytes') -and ($names -contains 'saves_bytes') -and ($names -contains 'investigators_bytes')) 'user.detail exposes byte-size metadata only'

Write-Host "[user.ban]" -ForegroundColor Yellow
$ban = Invoke-Api -Method POST -Path 'admin_api.php' -Query @{ action = 'user.ban' } -Token $adminToken -Body @{ user_id = $uuidC; banned = $true }
Assert-True ($ban.Status -eq 200 -and $ban.Body.status -eq 'success') 'user.ban sets banned=true'

$loginBanned = Invoke-Api -Method POST -Path 'db_api.php' -Query @{ action = 'login' } -Body @{ email = $emailC; password = $Password }
Assert-True ($loginBanned.Status -eq 403) 'banned user login returns 403'

$unban = Invoke-Api -Method POST -Path 'admin_api.php' -Query @{ action = 'user.ban' } -Token $adminToken -Body @{ user_id = $uuidC; banned = $false }
Assert-True ($unban.Status -eq 200 -and $unban.Body.status -eq 'success') 'user.ban sets banned=false'

$loginC = Invoke-Api -Method POST -Path 'db_api.php' -Query @{ action = 'login' } -Body @{ email = $emailC; password = $Password }
Assert-True ($loginC.Status -eq 200 -and $loginC.Body.auth_token) 'unbanned user login returns 200'
$tokenC = $loginC.Body.auth_token

Write-Host "[user.force_logout]" -ForegroundColor Yellow
$forceLogout = Invoke-Api -Method POST -Path 'admin_api.php' -Query @{ action = 'user.force_logout' } -Token $adminToken -Body @{ user_id = $uuidC }
Assert-True ($forceLogout.Status -eq 200 -and $forceLogout.Body.status -eq 'success') 'user.force_logout returns 200'

Write-Host "[user.set_role]" -ForegroundColor Yellow
$setAdmin = Invoke-Api -Method POST -Path 'admin_api.php' -Query @{ action = 'user.set_role' } -Token $adminToken -Body @{ user_id = $uuidC; role = 'admin' }
Assert-True ($setAdmin.Status -eq 200 -and $setAdmin.Body.status -eq 'success') 'user.set_role promotes C to admin'

$setUser = Invoke-Api -Method POST -Path 'admin_api.php' -Query @{ action = 'user.set_role' } -Token $adminToken -Body @{ user_id = $uuidC; role = 'user' }
Assert-True ($setUser.Status -eq 200 -and $setUser.Body.status -eq 'success') 'user.set_role demotes C back to user'

$selfRole = Invoke-Api -Method POST -Path 'admin_api.php' -Query @{ action = 'user.set_role' } -Token $adminToken -Body @{ user_id = $adminUuid; role = 'user' }
Assert-True ($selfRole.Status -eq 400) 'user.set_role on your own account returns 400'

Write-Host "[room.close]" -ForegroundColor Yellow
$roomId = 'ADM' + ([guid]::NewGuid().ToString('N').Substring(0, 5).ToUpper())
$roomCreate = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'create' } -Body @{ room_id = $roomId; user_id = $adminUuid; is_public = $true }
Assert-True ($roomCreate.Status -eq 200 -and $roomCreate.Body.status -eq 'success' -and $roomCreate.Body.host_token) 'room create returns host_token'

$roomsList = Invoke-Api -Method GET -Path 'admin_api.php' -Query @{ action = 'rooms.list' } -Token $adminToken
$roomFound = @($roomsList.Body.rooms | Where-Object { $_.room_id -eq $roomId })
Assert-True ($roomsList.Status -eq 200 -and $roomFound.Count -eq 1) 'rooms.list includes the new room'

$roomClose = Invoke-Api -Method POST -Path 'admin_api.php' -Query @{ action = 'room.close' } -Token $adminToken -Body @{ room_id = $roomId }
Assert-True ($roomClose.Status -eq 200 -and $roomClose.Body.status -eq 'success') 'room.close returns 200'

$roomsList2 = Invoke-Api -Method GET -Path 'admin_api.php' -Query @{ action = 'rooms.list' } -Token $adminToken
$roomFound2 = @($roomsList2.Body.rooms | Where-Object { $_.room_id -eq $roomId })
Assert-True ($roomFound2.Count -eq 0) 'rooms.list excludes the closed room'

Write-Host "[module.delete]" -ForegroundColor Yellow
$moduleTitle = "AdminSmoke-$([guid]::NewGuid().ToString('N').Substring(0,8))"
$modCreate = Invoke-Api -Method POST -Path 'library_api.php' -Query @{ action = 'create' } -Body @{ title = $moduleTitle; content = 'smoke module content'; author_id = $adminUuid; owner_token = (New-Hex -Bytes 32); type = 'original' }
Assert-True ($modCreate.Status -eq 200 -and $modCreate.Body.status -eq 'success' -and $modCreate.Body.id) 'library create returns module id'
$moduleId = $modCreate.Body.id

$modList = Invoke-Api -Method GET -Path 'admin_api.php' -Query @{ action = 'modules.list'; q = $moduleTitle } -Token $adminToken
$modFound = @($modList.Body.modules | Where-Object { $_.id -eq $moduleId })
Assert-True ($modList.Status -eq 200 -and $modFound.Count -eq 1) 'modules.list includes the new module'

$modDelete = Invoke-Api -Method POST -Path 'admin_api.php' -Query @{ action = 'module.delete' } -Token $adminToken -Body @{ id = $moduleId }
Assert-True ($modDelete.Status -eq 200 -and $modDelete.Body.status -eq 'success') 'module.delete returns 200'

$modList2 = Invoke-Api -Method GET -Path 'admin_api.php' -Query @{ action = 'modules.list'; q = $moduleTitle } -Token $adminToken
$modFound2 = @($modList2.Body.modules | Where-Object { $_.id -eq $moduleId })
Assert-True ($modFound2.Count -eq 0) 'modules.list excludes the deleted module'

Write-Host "[audit.list]" -ForegroundColor Yellow
$audit = Invoke-Api -Method GET -Path 'admin_api.php' -Query @{ action = 'audit.list' } -Token $adminToken
Assert-True ($audit.Status -eq 200 -and $audit.Body.total -ge 1) 'audit.list returns at least one entry'
$actions = @($audit.Body.entries | ForEach-Object { $_.action })
Assert-True (($actions -contains 'user.ban') -or ($actions -contains 'user.force_logout') -or ($actions -contains 'user.set_role') -or ($actions -contains 'room.close') -or ($actions -contains 'module.delete')) 'audit.list includes a recent admin mutation'

Write-Host "[batch delete/close]" -ForegroundColor Yellow
$emailD1 = "admin-d1+$([guid]::NewGuid().ToString('N').Substring(0,8))@example.com"
$emailD2 = "admin-d2+$([guid]::NewGuid().ToString('N').Substring(0,8))@example.com"
$regD1 = Invoke-Api -Method POST -Path 'db_api.php' -Query @{ action = 'register' } -Body @{ email = $emailD1; password = $Password }
$regD2 = Invoke-Api -Method POST -Path 'db_api.php' -Query @{ action = 'register' } -Body @{ email = $emailD2; password = $Password }
Assert-True ($regD1.Status -eq 200 -and $regD1.Body.user_id) 'register batch user D1 returns user_id'
Assert-True ($regD2.Status -eq 200 -and $regD2.Body.user_id) 'register batch user D2 returns user_id'
$uuidD1 = $regD1.Body.user_id
$uuidD2 = $regD2.Body.user_id

$listD1 = Invoke-Api -Method GET -Path 'admin_api.php' -Query @{ action = 'users.list'; q = $emailD1 } -Token $adminToken
$listD2 = Invoke-Api -Method GET -Path 'admin_api.php' -Query @{ action = 'users.list'; q = $emailD2 } -Token $adminToken
Assert-True (@($listD1.Body.users | Where-Object { $_.uuid -eq $uuidD1 }).Count -eq 1) 'users.list includes batch user D1'
Assert-True (@($listD2.Body.users | Where-Object { $_.uuid -eq $uuidD2 }).Count -eq 1) 'users.list includes batch user D2'

$delBatch = Invoke-Api -Method POST -Path 'admin_api.php' -Query @{ action = 'user.delete' } -Token $adminToken -Body @{ user_ids = @($uuidD1, $uuidD2) }
Assert-True ($delBatch.Status -eq 200 -and $delBatch.Body.status -eq 'success' -and $delBatch.Body.deleted -eq 2) 'user.delete batch removes 2 users'

$listD1b = Invoke-Api -Method GET -Path 'admin_api.php' -Query @{ action = 'users.list'; q = $emailD1 } -Token $adminToken
$listD2b = Invoke-Api -Method GET -Path 'admin_api.php' -Query @{ action = 'users.list'; q = $emailD2 } -Token $adminToken
Assert-True (@($listD1b.Body.users | Where-Object { $_.uuid -eq $uuidD1 }).Count -eq 0) 'users.list excludes deleted user D1'
Assert-True (@($listD2b.Body.users | Where-Object { $_.uuid -eq $uuidD2 }).Count -eq 0) 'users.list excludes deleted user D2'

$delSelf = Invoke-Api -Method POST -Path 'admin_api.php' -Query @{ action = 'user.delete' } -Token $adminToken -Body @{ user_id = $adminUuid }
Assert-True ($delSelf.Status -eq 400 -and $delSelf.Body.message -eq 'No valid users to delete') 'user.delete on own account returns 400'

$roomId1 = 'BT1' + ([guid]::NewGuid().ToString('N').Substring(0, 5).ToUpper())
$roomId2 = 'BT2' + ([guid]::NewGuid().ToString('N').Substring(0, 5).ToUpper())
$createR1 = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'create' } -Body @{ room_id = $roomId1; user_id = $adminUuid; is_public = $true }
$createR2 = Invoke-Api -Method POST -Path 'room_api.php' -Query @{ action = 'create' } -Body @{ room_id = $roomId2; user_id = $adminUuid; is_public = $true }
Assert-True ($createR1.Status -eq 200 -and $createR1.Body.host_token) 'batch room R1 create returns host_token'
Assert-True ($createR2.Status -eq 200 -and $createR2.Body.host_token) 'batch room R2 create returns host_token'

$roomsBatch = Invoke-Api -Method GET -Path 'admin_api.php' -Query @{ action = 'rooms.list' } -Token $adminToken
$batchFound = @($roomsBatch.Body.rooms | Where-Object { $_.room_id -eq $roomId1 -or $_.room_id -eq $roomId2 })
Assert-True ($batchFound.Count -eq 2) 'rooms.list includes both batch rooms'

$closeBatch = Invoke-Api -Method POST -Path 'admin_api.php' -Query @{ action = 'room.close' } -Token $adminToken -Body @{ room_ids = @($roomId1, $roomId2) }
Assert-True ($closeBatch.Status -eq 200 -and $closeBatch.Body.status -eq 'success' -and $closeBatch.Body.closed -eq 2) 'room.close batch closes 2 rooms'

$roomsBatch2 = Invoke-Api -Method GET -Path 'admin_api.php' -Query @{ action = 'rooms.list' } -Token $adminToken
$batchFound2 = @($roomsBatch2.Body.rooms | Where-Object { $_.room_id -eq $roomId1 -or $_.room_id -eq $roomId2 })
Assert-True ($batchFound2.Count -eq 0) 'rooms.list excludes both closed batch rooms'

Write-Host "[me]" -ForegroundColor Yellow
$meAdmin = Invoke-Api -Method GET -Path 'db_api.php' -Query @{ action = 'me' } -Token $adminToken
Assert-True ($meAdmin.Status -eq 200 -and $meAdmin.Body.role -eq 'admin' -and $meAdmin.Body.user_id) 'me with admin token returns role=admin and user_id'
Assert-True ($meAdmin.Body.user_id -eq $adminUuid) 'me admin user_id matches the logged-in admin'

# C's token was invalidated by force_logout earlier; log in again for a fresh token.
$loginC2 = Invoke-Api -Method POST -Path 'db_api.php' -Query @{ action = 'login' } -Body @{ email = $emailC; password = $Password }
$tokenC = $loginC2.Body.auth_token
$meUser = Invoke-Api -Method GET -Path 'db_api.php' -Query @{ action = 'me' } -Token $tokenC
Assert-True ($meUser.Status -eq 200 -and $meUser.Body.role -eq 'user') 'me with normal user token returns role=user'

$meNoToken = Invoke-Api -Method GET -Path 'db_api.php' -Query @{ action = 'me' }
Assert-True ($meNoToken.Status -eq 401) 'me without token returns 401'

Write-Host ""
$color = if ($script:Fail -eq 0) { 'Green' } else { 'Red' }
Write-Host ("Result: {0} passed, {1} failed" -f $script:Pass, $script:Fail) -ForegroundColor $color
if ($script:Fail -gt 0) { exit 1 }
exit 0
