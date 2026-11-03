# cloud-server Docker 本地运行命令

以下命令均在目录 `E:\Robot\apps\cloud-server` 下执行。

## 进入工程目录

```powershell
cd E:\Robot\apps\cloud-server
```

## 首次启动

首次运行或修改了代码、Dockerfile、依赖、`.env`、Compose 配置时使用：

```powershell
docker compose --env-file .env -f docker-compose.dev.yml up --build -d
```

说明：

- `--build`：重新构建 `cloud-server` 镜像。
- `-d`：后台运行。
- `cloud-server` 镜像会在一次构建中编译 `admin-web` 和 Go 服务端；浏览器访问 `http://localhost:8080` 打开管理员后台。
- MySQL 第一次启动时自动创建数据库和业务账号。
- `cloud-server` 会等待 MySQL 健康检查通过后启动。
- 服务启动时自动创建授权模块数据表。

## 前台启动并查看实时日志

```powershell
docker compose --env-file .env -f docker-compose.dev.yml up --build
```

按 `Ctrl+C` 停止前台运行。

## 已创建容器的启动

如果容器已经创建过，只是被停止，可以使用：

```powershell
docker compose --env-file .env -f docker-compose.dev.yml start
```

`start` 不会重新构建镜像，也不会应用新的代码、`.env` 或 Compose 配置。

## 停止服务

停止容器但保留容器和 MySQL 数据：

```powershell
docker compose --env-file .env -f docker-compose.dev.yml stop
```

停止并删除容器，但保留 MySQL 数据卷：

```powershell
docker compose --env-file .env -f docker-compose.dev.yml down
```

## 重启服务

普通重启：

```powershell
docker compose --env-file .env -f docker-compose.dev.yml restart
```

修改代码或环境变量后重新构建并启动：

```powershell
docker compose --env-file .env -f docker-compose.dev.yml up --build -d
```

## 查看状态

```powershell
docker compose --env-file .env -f docker-compose.dev.yml ps
```

## 查看日志

查看所有服务日志：

```powershell
docker compose --env-file .env -f docker-compose.dev.yml logs
```

持续查看 `cloud-server` 日志：

```powershell
docker compose --env-file .env -f docker-compose.dev.yml logs -f cloud-server
```

持续查看 MySQL 日志：

```powershell
docker compose --env-file .env -f docker-compose.dev.yml logs -f mysql
```

## 检查服务

```powershell
Invoke-WebRequest http://localhost:8080/healthz -Method Get
```

正常时返回 HTTP `204 No Content`。

## 重新构建服务镜像

```powershell
docker compose --env-file .env -f docker-compose.dev.yml build --no-cache cloud-server
docker compose --env-file .env -f docker-compose.dev.yml up -d
```

## 清空本地数据库并重新初始化

警告：下面命令会删除 Docker volume 中的 MySQL 数据，包括管理员账号、代理商账号、会话和审计日志。

```powershell
docker compose --env-file .env -f docker-compose.dev.yml down -v
docker compose --env-file .env -f docker-compose.dev.yml up --build -d
```

## 查看容器内部环境和文件

进入 `cloud-server` 容器：

```powershell
docker compose --env-file .env -f docker-compose.dev.yml exec cloud-server sh
```

进入 MySQL 容器：

```powershell
docker compose --env-file .env -f docker-compose.dev.yml exec mysql mysql -uroot -p robot
```

## 常见判断

如果执行 `start` 报容器不存在，改用：

```powershell
docker compose --env-file .env -f docker-compose.dev.yml up --build -d
```

如果修改 `.env` 后配置没有生效，执行：

```powershell
docker compose --env-file .env -f docker-compose.dev.yml up --build -d --force-recreate
```
