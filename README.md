## SRP（Simple Reverse Proxy 简易反向代理）

### 1.简介

TCP、UDP反向代理（内网穿透）和协议转换工具

### 2.用法

srp-server端：

```shell
Usage of server.exe:
  -client-ip string
        srp-client连接的IP地址 (default "0.0.0.0")
  -client-port int
        srp-client连接的端口 (default 6352)
  -log-level int
        日志级别（1-3） (default 2)
  -protocol string
        用户和srp-server间的通信协议，支持：tcp协议，udp协议 (default "tcp")
  -server-ip string
        用户访问被转发服务的IP地址 (default "0.0.0.0")
  -server-pwd string
        srp-server连接密码 (default "default_password")
  -user-port int
        用户访问被转发服务的端口 (default 9352)
  -version
        打印版本信息
```

srp-client端：

```shell
Usage of client.exe:
  -log-level int
        日志级别（1-3） (default 2)
  -protocol string
        srp-client和被转发服务的通信协议，支持：tcp协议，udp协议 (default "tcp")
  -server-ip string
        srp-server的IP地址 (default "127.0.0.1")
  -server-port int
        srp-server监听的端口 (default 6352)
  -server-pwd string
        连接srp-server的密码 (default "default_password")
  -service-ip string
        被转发服务的IP地址 (default "127.0.0.1")
  -service-port int
        被转发服务的端口 (default 80)
  -version
        打印版本信息
```

### 3.原理

`service <---> srp-server <---> srp-client <---> User`

### 4.下载

发布页：[srp releases](https://github.com/paoka1/srp/releases)，下载的版本可能落后于手动构建的版本

### 5.构建

切换目录至`build`运行对应的构建脚本，构建的二进制文件会被输出到根目录的`bin`目录

或在项目根目录运行构建命令：

```shell
go build -o client cmd/client/main.go
go build -o server cmd/server/main.go
```

### 6.实例

#### 6.1内网穿透转发WEB服务

转发192.168.12.172（内网）的WEB服务到远程服务器（具有公网IP地址A）：

1.在远程服务器运行srp服务端：

```shell
./server
```

2.在192.168.12.172运行srp客户端：

```shell
./client -server-ip A
```

3.访问被转发的WEB服务：

```shell
curl A:9352
```

#### 6.2内网穿透转发SSH服务

转发192.168.12.172（内网）的SSH服务到远程服务器（具有公网IP地址A）：

1.在远程服务器运行srp服务端：

```shell
./server
```

2.在192.168.12.172运行srp客户端：

```shell
./client -server-ip A -service-port 22
```

3.连接被转发的SSH服务：

```shell
ssh -p 9352 username@A
```

注意：

1.参数`-user-port`为用户连接的端口，`-client-port`为srp客户端连接的端口

2.参数`-server-port`和`-server-pwd`均与srp服务端有关，`-service-ip`和`-service-port`均和被转发服务有关

3.在srp-server和srp-client指定不同的protocol即可实现协议的转换

4.必要时修改默认连接密码

### 7.关于

该项目计划仅支持TCP、UDP协议，后续的更新维护内容主要为性能优化以及BUG修复
