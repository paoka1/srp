### Simple Reverse Proxy（srp 简易反向代理）

#### 1.功能
TCP反向代理
#### 2.用法
```
Usage of server.exe:
  -client-port int
        srp-client连接的端口 (default 6000)
  -isDebug
        开启调试模式
  -server-pwd string
        访问转发服务的密码 (default "default_password")
  -user-port int
        用户访问转发服务的端口 (default 9000)
        
Usage of client.exe:
  -debug
        开启调试模式
  -server-ip string
        srp-server的IP地址 (default "127.0.0.1")
  -server-port int
        srp-server监听的端口 (default 6000)
  -server-pwd string
        连接srp-server的密码 (default "default_password")
  -service-ip string
        被转发服务的IP地址 (default "127.0.0.1")
  -service-port int
        被转发服务的端口 (default 3000)
```

#### 3.原理
service <---> srp-Server <---> srp-Client <---> User
