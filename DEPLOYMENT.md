# kvix 部署文档

本文档记录如何把 `kvix` 的 HTTP 示例服务部署成一个可长期运行、可重启后保留数据的 Linux 服务。

当前项目最适合部署的入口是：

```bash
./http
```

部署后的运行目标是：

- 以 `systemd` 服务方式启动
- 监听固定地址和端口
- 将底层数据库落到固定目录，而不是临时目录
- 支持重启后保留数据

## 1. 部署前提

建议环境：

- Linux x86_64
- `systemd`
- 可通过 SSH 登录目标机器

项目运行入口依赖以下环境变量：

- `KVIX_HTTP_ADDR`
- `KVIX_HTTP_DATA_DIR`

其中：

- `KVIX_HTTP_ADDR` 控制 HTTP 监听地址
- `KVIX_HTTP_DATA_DIR` 控制 `kvix` 的持久化数据目录

如果不设置 `KVIX_HTTP_DATA_DIR`，HTTP 服务会退回示例模式，自动创建临时目录，进程退出后数据会丢失。因此服务器部署必须配置这个变量。

## 2. 推荐部署目录

本文档采用以下目录约定：

```text
/opt/kvix/bin/kvix-http           # 服务二进制
/etc/kvix/kvix-http.env           # 环境变量文件
/etc/systemd/system/kvix.service  # systemd 单元文件
/var/lib/kvix-http/data           # 实际数据目录
```

这套目录划分的好处是：

- 二进制和数据分离
- 环境变量集中管理
- 重启、升级和回滚路径清晰

## 3. 本地编译 Linux 二进制

如果目标机器没有 Go 环境，最简单的方式是直接在本地交叉编译。

在仓库根目录执行：

```bash
mkdir -p dist
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOCACHE=$(pwd)/.cache/go \
  go build -trimpath -o dist/kvix-http ./http
```

可选验证：

```bash
file dist/kvix-http
```

预期结果应包含：

- `ELF 64-bit`
- `x86-64`

## 4. 上传二进制到服务器

先在服务器上创建目录：

```bash
mkdir -p /opt/kvix/bin /etc/kvix /var/lib/kvix-http/data
```

然后从本地上传：

```bash
scp -P 22 dist/kvix-http root@<server-ip>:/opt/kvix/bin/kvix-http
```

上传后在服务器上赋予执行权限：

```bash
chmod 755 /opt/kvix/bin/kvix-http
```

## 5. 配置环境变量文件

在服务器上创建：

```bash
cat > /etc/kvix/kvix-http.env <<'EOF'
KVIX_HTTP_ADDR=0.0.0.0:8080
KVIX_HTTP_DATA_DIR=/var/lib/kvix-http/data
EOF
```

这两个变量的意义分别是：

- `0.0.0.0:8080`：监听所有网卡，方便外部访问
- `/var/lib/kvix-http/data`：将底层数据库持久化到固定目录

如果你希望只允许本机访问，可以改成：

```text
127.0.0.1:8080
```

## 6. 配置 systemd 服务

在服务器上创建：

```bash
cat > /etc/systemd/system/kvix.service <<'EOF'
[Unit]
Description=kvix HTTP service
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/kvix
EnvironmentFile=/etc/kvix/kvix-http.env
ExecStart=/opt/kvix/bin/kvix-http
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF
```

然后执行：

```bash
systemctl daemon-reload
systemctl enable --now kvix.service
```

## 7. 部署后验证

### 7.1 查看服务状态

```bash
systemctl --no-pager --full status kvix.service
```

### 7.2 查看健康检查

在服务器本机执行：

```bash
curl http://127.0.0.1:8080/healthz
```

预期返回：

```json
{"code":200,"message":"ok","data":{"status":"ok"}}
```

### 7.3 验证数据持久化

先写入一条记录：

```bash
curl -X POST http://127.0.0.1:8080/api/v1/entries \
  -H 'Content-Type: application/json' \
  -d '{"key":"deploy-check","value":"ok"}'
```

然后重启服务：

```bash
systemctl restart kvix.service
```

重启后再次读取：

```bash
curl http://127.0.0.1:8080/api/v1/entries/deploy-check
```

如果仍然能读到原值，说明：

- `KVIX_HTTP_DATA_DIR` 生效
- 数据没有落到临时目录
- 服务重启后状态能够保留

## 8. 升级方式

后续升级推荐流程：

1. 在本地重新编译新版本 `dist/kvix-http`
2. 上传覆盖服务器上的 `/opt/kvix/bin/kvix-http`
3. 执行 `systemctl restart kvix.service`
4. 再跑一次健康检查和关键接口验证

示例：

```bash
scp -P 22 dist/kvix-http root@<server-ip>:/opt/kvix/bin/kvix-http
ssh root@<server-ip> 'chmod 755 /opt/kvix/bin/kvix-http && systemctl restart kvix.service'
```

## 9. 回滚与排障

### 9.1 查看日志

```bash
journalctl -u kvix.service -n 100 --no-pager
```

### 9.2 常见问题

#### 服务能启动，但数据重启后丢失

通常原因是没有设置：

```text
KVIX_HTTP_DATA_DIR
```

#### 本机可访问，外部不可访问

需要检查：

- `KVIX_HTTP_ADDR` 是否为 `0.0.0.0:8080`
- 操作系统防火墙是否放行 `8080`
- 云厂商安全组是否放行 `8080`

#### 服务起不来

优先检查：

- 二进制是否存在且可执行
- `EnvironmentFile` 路径是否正确
- 数据目录是否可写
- `journalctl -u kvix.service` 输出

## 10. 本次部署采用的实际配置

本次实际部署采用的是：

- 服务名：`kvix.service`
- 二进制路径：`/opt/kvix/bin/kvix-http`
- 环境文件：`/etc/kvix/kvix-http.env`
- 数据目录：`/var/lib/kvix-http/data`
- 监听地址：`0.0.0.0:8080`

如果你要在其他机器复用这套部署方式，通常只需要改：

- 服务器地址
- SSH 用户
- 端口
- 需要开放的监听端口
