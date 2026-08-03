# Docker Hub 镜像标签搜索脚本
# 支持搜索、排序、分页等功能

# 设置代理（如果需要）
$env:HTTP_PROXY = "http://192.168.0.225:7892"
$env:HTTPS_PROXY = "http://192.168.0.225:7892"

function Get-DockerTags {
    param(
        [Parameter(Mandatory=$true)]
        [string]$ImageName,
        
        [string]$Username = "",
        
        [string]$Search = "",
        
        [int]$Count = 10,
        
        [ValidateSet("name", "size", "date")]
        [string]$SortBy = "date",
        
        [bool]$Descending = $true,
        
        [switch]$All,
        
        [switch]$ShowOS,
        
        [string]$OutputFile = ""
    )
    
    # 解析镜像名称，支持 user/image 格式
    if ($ImageName -like "*/*") {
        $parts = $ImageName -split "/", 2
        $actualUsername = $parts[0]
        $actualImageName = $parts[1]
    } else {
        $actualUsername = if ($Username) { $Username } else { "library" }
        $actualImageName = $ImageName
    }
    
    Write-Host "获取 $actualUsername/$actualImageName 镜像标签..." -ForegroundColor Cyan
    
    $allTags = @()
    $page = 1
    $pageSize = 100
    
    # 获取所有标签数据
    do {
        try {
            $uri = "https://hub.docker.com/v2/repositories/$actualUsername/$actualImageName/tags/?page=$page" + "`&page_size=$pageSize"
            Write-Host "获取第 $page 页数据..." -ForegroundColor Yellow
            
            $response = Invoke-WebRequest -Uri $uri -UseBasicParsing
            $data = $response.Content | ConvertFrom-Json
            
            if ($data.results.Count -eq 0) {
                break
            }
            
            foreach ($tag in $data.results) {
                # 提取架构信息
                $archList = @()
                $linuxArchList = @()  # 用于不显示OS时的Linux架构筛选
                
                if ($tag.images -and $tag.images.Count -gt 0) {
                    foreach ($image in $tag.images) {
                        if ($image.architecture -and $image.architecture -ne "unknown") {
                            $arch = $image.architecture
                            if ($image.variant) {
                                $arch += "/$($image.variant)"
                            }
                            
                            $os = if ($image.os) { $image.os } else { "linux" }
                            
                            if ($ShowOS) {
                                # 显示OS时，非Linux的架构需要加OS前缀
                                if ($os -ne "linux") {
                                    $archWithOS = "$os/$arch"
                                } else {
                                    $archWithOS = $arch
                                }
                                $archList += $archWithOS
                            } else {
                                # 不显示OS时，只收集Linux架构
                                if ($os -eq "linux") {
                                    $linuxArchList += $arch
                                }
                            }
                        }
                    }
                }
                
                # 根据ShowOS参数决定使用哪个架构列表
                $finalArchList = if ($ShowOS) { $archList } else { $linuxArchList }
                
                $tagInfo = [PSCustomObject]@{
                    Tag = $tag.name
                    Status = $tag.tag_status
                    Size = [math]::Round($tag.full_size/1024/1024, 2)
                    Architecture = ($finalArchList | Sort-Object -Unique) -join ", "
                    LastUpdated = $tag.last_updated
                }
                
                $allTags += $tagInfo
            }
            
            $page++
            Start-Sleep -Milliseconds 500
        }
        catch {
            Write-Error "获取数据失败: $_"
            break
        }
    } while ($data.next)
    
    # 应用搜索过滤
    if ($Search) {
        $allTags = $allTags | Where-Object { $_.Tag -like "*$Search*" }
    }
    
    # 排序
    switch ($SortBy) {
        "name" { 
            if ($Descending) {
                $allTags = $allTags | Sort-Object -Property Tag -Descending
            } else {
                $allTags = $allTags | Sort-Object -Property Tag
            }
        }
        "size" { 
            if ($Descending) {
                $allTags = $allTags | Sort-Object -Property Size -Descending
            } else {
                $allTags = $allTags | Sort-Object -Property Size
            }
        }
        "date" { 
            if ($Descending) {
                $allTags = $allTags | Sort-Object -Property LastUpdated -Descending
            } else {
                $allTags = $allTags | Sort-Object -Property LastUpdated
            }
        }
    }
    
    # 确定显示的标签
    if ($All) {
        $displayTags = $allTags
    } else {
        $displayTags = $allTags | Select-Object -First $Count
    }
    
    Write-Host ""
    Write-Host "镜像: $actualUsername/$actualImageName" -ForegroundColor Green
    Write-Host ""
    Write-Host "找到 $($allTags.Count) 个标签" -ForegroundColor Green
    if (-not $All) {
        Write-Host "显示前 $Count 个标签" -ForegroundColor Yellow
    }
    Write-Host ""
    
    # 显示结果
    $displayTags | Format-Table -AutoSize
    
    # 提示信息
    if (-not $All -and $allTags.Count -gt $Count) {
        Write-Host "`n提示: 使用 -All 参数查看所有 $($allTags.Count) 个标签" -ForegroundColor Yellow
    }
    
    # 如果指定了输出文件，则保存标签信息到文件
    if ($OutputFile) {
        try {
            # 创建文件头部信息
            $fileContent = @()
            $fileContent += "# Docker Hub 镜像标签信息"
            $fileContent += "# 镜像: $actualUsername/$actualImageName"
            $fileContent += "# 生成时间: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')"
            $fileContent += "# 排序方式: $SortBy $(if ($Descending) { '(降序)' } else { '(升序)' })"
            if ($Search) {
                $fileContent += "# 搜索关键词: $Search"
            }
            $fileContent += "# 共 $($allTags.Count) 个标签"
            if (-not $All) {
                $fileContent += "# 显示前 $Count 个标签"
            }
            $fileContent += ""
            
            # 添加表格头
            $fileContent += "Tag`tStatus`tSize(MB)`tArchitecture`tLastUpdated"
            $fileContent += "---`t------`t--------`t------------`t-----------"
            
            # 添加标签数据
            foreach ($tag in $displayTags) {
                $fileContent += "$($tag.Tag)`t$($tag.Status)`t$($tag.Size)`t$($tag.Architecture)`t$($tag.LastUpdated)"
            }
            
            # 写入文件
            $fileContent | Out-File -FilePath $OutputFile -Encoding UTF8
            Write-Host ""
            Write-Host "标签信息已保存到文件: $OutputFile" -ForegroundColor Green
        }
        catch {
            Write-Error "保存文件失败: $_"
        }
    }
}

# 新增功能：提取指定镜像的 amd64 和 arm64 架构标签
function Get-DockerArchImages {
    param(
        [Parameter(Mandatory=$true)]
        [string]$ImageName,
        
        [string]$Username = "",
        
        [int]$Count = 10,
        
        [switch]$All,
        
        [switch]$ShowOS,
        
        [string]$OutputFile = ""
    )
    
    # 解析镜像名称，支持 user/image 格式
    if ($ImageName -like "*/*") {
        $parts = $ImageName -split "/", 2
        $actualUsername = $parts[0]
        $actualImageName = $parts[1]
    } else {
        $actualUsername = if ($Username) { $Username } else { "library" }
        $actualImageName = $ImageName
    }
    
    Write-Host "获取 $actualUsername/$actualImageName 镜像的 amd64 和 arm64 架构信息..." -ForegroundColor Cyan
    
    $allTags = @()
    $page = 1
    $pageSize = 100
    
    # 获取所有标签数据
    do {
        try {
            $uri = "https://hub.docker.com/v2/repositories/$actualUsername/$actualImageName/tags/?page=$page" + "`&page_size=$pageSize"
            Write-Host "获取第 $page 页数据..." -ForegroundColor Yellow
            
            $response = Invoke-WebRequest -Uri $uri -UseBasicParsing
            $data = $response.Content | ConvertFrom-Json
            
            if ($data.results.Count -eq 0) {
                break
            }
            
            foreach ($tag in $data.results) {
                # 提取架构信息
                $archList = @()
                $linuxArchList = @()  # 用于不显示OS时的Linux架构筛选
                $linuxAmd64 = $false
                $linuxArm64 = $false
                
                if ($tag.images -and $tag.images.Count -gt 0) {
                    foreach ($image in $tag.images) {
                        if ($image.architecture -and $image.architecture -ne "unknown") {
                            $arch = $image.architecture
                            if ($image.variant) {
                                $arch += "/$($image.variant)"
                            }
                            
                            $os = if ($image.os) { $image.os } else { "linux" }
                            
                            if ($ShowOS) {
                                # 显示OS时，非Linux的架构需要加OS前缀
                                if ($os -ne "linux") {
                                    $archWithOS = "$os/$arch"
                                } else {
                                    $archWithOS = $arch
                                }
                                $archList += $archWithOS
                            } else {
                                # 不显示OS时，只收集Linux架构
                                if ($os -eq "linux") {
                                    $linuxArchList += $arch
                                }
                            }
                            
                            # 检查Linux下的amd64和arm64架构（用于筛选）
                            if ($os -eq "linux") {
                                if ($image.architecture -eq "amd64") {
                                    $linuxAmd64 = $true
                                }
                                if ($image.architecture -eq "arm64" -or ($image.architecture -eq "arm64" -and $image.variant -eq "v8")) {
                                    $linuxArm64 = $true
                                }
                            }
                        }
                    }
                }
                
                # 根据ShowOS参数决定使用哪个架构列表
                $finalArchList = if ($ShowOS) { $archList } else { $linuxArchList }
                
                # 检查是否包含 amd64 或 arm64 架构（仅限Linux）
                $hasAmd64 = $linuxAmd64
                $hasArm64 = $linuxArm64
                
                if ($hasAmd64 -or $hasArm64) {
                    $tagInfo = [PSCustomObject]@{
                        Tag = $tag.name
                        Status = $tag.tag_status
                        Size = [math]::Round($tag.full_size/1024/1024, 2)
                        Architecture = ($finalArchList | Sort-Object -Unique) -join ", "
                        LastUpdated = $tag.last_updated
                        HasAmd64 = $hasAmd64
                        HasArm64 = $hasArm64
                    }
                    
                    $allTags += $tagInfo
                }
            }
            
            $page++
            Start-Sleep -Milliseconds 500
        }
        catch {
            Write-Error "获取数据失败: $_"
            break
        }
    } while ($data.next)
    
    # 按时间倒序排序
    $allTags = $allTags | Sort-Object -Property LastUpdated -Descending
    
    # 确定显示的标签
    if ($All) {
        $displayTags = $allTags
    } else {
        $displayTags = $allTags | Select-Object -First $Count
    }
    
    Write-Host ""
    Write-Host "镜像: $actualUsername/$actualImageName" -ForegroundColor Green
    Write-Host ""
    Write-Host "找到 $($allTags.Count) 个包含 amd64 或 arm64 架构的标签" -ForegroundColor Green
    if (-not $All) {
        Write-Host "显示前 $Count 个标签" -ForegroundColor Yellow
    }
    Write-Host ""
    
    # 显示结果表格
    $displayTags | Select-Object Tag, Status, Size, Architecture, LastUpdated | Format-Table -AutoSize
    
    # 提示信息
    if (-not $All -and $allTags.Count -gt $Count) {
        Write-Host "`n提示: 使用 -All 参数查看所有 $($allTags.Count) 个标签" -ForegroundColor Yellow
    }
    
    # 生成 Docker 命令列表
    Write-Host "=== Docker 镜像拉取命令 ===" -ForegroundColor Yellow
    $dockerCommands = @()
    # 为 library 镜像省略 library/ 前缀
    $dockerImageName = if ($actualUsername -eq "library") { $actualImageName } else { "$actualUsername/$actualImageName" }
    
    foreach ($tag in $displayTags) {
        $tagName = $tag.Tag
        
        if ($tag.HasAmd64) {
            # $dockerCommands += "docker pull $dockerImageName`:$tagName"
            $dockerCommands += "$dockerImageName`:$tagName"
        }
        
        if ($tag.HasArm64) {
            #$dockerCommands += "docker pull --platform=linux/arm64 $dockerImageName`:$tagName"
            $dockerCommands += "--platform=linux/arm64 $dockerImageName`:$tagName"
        }
    }
    
    # 输出所有命令到控制台
    foreach ($cmd in $dockerCommands) {
        Write-Host $cmd -ForegroundColor White
    }
    
    # 如果指定了输出文件，则保存命令到文件
    if ($OutputFile) {
        try {
            # 创建文件头部信息
            $fileContent = @()
            $fileContent += "# Docker Hub 镜像拉取命令"
            $fileContent += "# 镜像: $dockerImageName"
            $fileContent += "# 生成时间: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')"
            $fileContent += "# 共 $($dockerCommands.Count) 条命令"
            $fileContent += ""
            
            # 添加所有命令
            $fileContent += $dockerCommands
            
            # 写入文件
            $fileContent | Out-File -FilePath $OutputFile -Encoding UTF8
            Write-Host ""
            Write-Host "命令已保存到文件: $OutputFile" -ForegroundColor Green
        }
        catch {
            Write-Error "保存文件失败: $_"
        }
    }
    
    Write-Host ""
    Write-Host "共生成 $($dockerCommands.Count) 条 Docker 命令" -ForegroundColor Green
}