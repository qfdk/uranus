### Nginx Proxy Manager (NPM)

Gin 框架写的 NGINX 代理设置工具，

自动读取NGINX配置文件， 并且可以在线操作，目的是可以随时添加站点，取代宝塔面板。

已经完成：

- 读取nginx版本
- nginx 关闭/重启/载入配置操作
- 添加网站 并自动申请证书
- SSL 自自动签名

更改中：

- redis key 结构
- npm(nginx-proxy-manager)
    - 域名
        - 证书
            - 私钥
            - 公钥
        - 配置文件(.conf)