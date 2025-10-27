### SRP（Simple Reverse Proxy 简易反向代理）

#### 1.功能
TCP反向代理
#### 2.用法
```
Usage of server.exe:
  -client-ip string
        srp-client连接的IP地址 (default "0.0.0.0")
  -client-port int
        srp-client连接的端口 (default 6352)
  -log-level int
        日志级别（1-3） (default 2)
  -protocol string
        srp-client和转发服务间通信的协议，支持：tcp协议 (default "tcp")
  -server-ip string
        用户访问的IP地址 (default "0.0.0.0")
  -server-pwd string
        访问转发服务的密码 (default "default_password")
  -user-port int
        用户访问转发服务的端口 (default 9352)
        
Usage of client.exe:
  -log-level int
        日志级别（1-3） (default 2)
  -protocol string
        用户和srp-server间通信的协议，支持：tcp协议 (default "tcp")
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
```

#### 3.原理
`service <---> srp-server <---> srp-client <---> User`

#### 4.构建

切换目录至`build`运行对应的构建脚本（仅构建linux amd64、windows amd64），构建完成后，二进制文件会被输出到`bin`目录

#### 5.例子

转发192.168.12.172的ssh服务到192.168.12.1的22端口：

1.在192.168.12.1运行srp服务端：

```shell
server.exe -user-port 22
```

2.在192.168.12.172运行srp客户端：

```shell
./client -server-ip 192.168.12.1 -service-port 22
```

3.连接192.168.12.172的ssh服务：

```shell
ssh ubuntu@192.168.12.1
```

注意：

1.参数`-user-port`为用户连接的端口，`-client-port`为srp客户端连接的端口

2.参数`-server-port`和`-server-pwd`均与srp服务端有关，`-service-ip`和`-service-port`均和被转发服务有关

#### 6.关于

该项目仍在开发中，不免存在bug，后续也许会支持更多协议
