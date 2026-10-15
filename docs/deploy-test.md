# 通用 Linux 容器：测试环境打包与配置

本说明只涉及应用产物、测试配置和启动方式，不安装 MySQL、Redis、Docker 或 K8s。
前端构建后由 Go 服务提供页面和 API；默认不需要单独的前端容器或 Nginx。

## 1. 配置文件在哪里

| 文件 | 用途 | 测试环境如何使用 |
| --- | --- | --- |
| conf/app.conf | 默认后端配置，也提供部分前端页面配置 | 不修改仓库开发配置；部署目录中用测试模板替换它 |
| conf/app-test.conf | 非敏感测试配置模板 | 改好域名、库名，再复制/挂载成容器工作目录下的 conf/app.conf |
| conf/runtime-test.env.example | 数据库、Redis 和可选业务变量模板 | 复制成 conf/runtime-test.env，填写后通过平台注入；真实文件已加入 .gitignore |
| web/vite.config.ts | 前端构建和开发代理配置 | 同域容器部署无需修改；CASDOOR_BACKEND 仅控制 Vite 开发代理 |
| Casdoor 管理页面 / 数据库 | Organization、Application、OIDC 回调、邮件/短信/存储 Provider | 登录测试环境后配置，不是修改前端源码 |

本方案**不需要 web/.env.test**：前端使用相对地址访问同域 API，没有读取 VITE_API_URL 等部署变量。
同一份 web/build 可以用于不同环境；只修改 originFrontend 不会让静态前端自动跨域调用后端。
若另行拆分部署，仍须配置反向代理保持浏览器同域，或调整实现。

## 2. 前后端打包命令

推荐与现有 Dockerfile 对齐：Go 1.25.8、Node.js 20.20.1；前端使用 Yarn Classic 1.x（如 1.22.22）。
仅构建需要这些工具，运行容器不需要安装 Go、Node.js 或 Yarn。

在项目根目录执行前端打包：

    cd web
    yarn install --frozen-lockfile
    yarn build
    cd ..

产物：**web/build/**。必须用 yarn build，它会执行 postbuild，把 build-temp 切换成最终 build。
不要只执行 vite build，也不要把 yarn dev 当成容器部署启动命令。

在项目根目录执行后端打包（Linux x86_64 / amd64）：

    go mod download
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
      go build -trimpath -ldflags="-w -s" -o bin/server .

产物：**bin/server**。ARM64 容器将 GOARCH=amd64 改成 GOARCH=arm64。
目标架构取决于运行容器的宿主机，而不是打包电脑；Linux 二进制不能直接在 macOS 运行。
bash build.sh 也可以打包，但它会同时构建 Linux amd64 和 arm64 两个版本。

## 3. 测试环境需要改哪些配置

### 必须填写

| 配置项 | 文件 | 填写方式 |
| --- | --- | --- |
| driverName | conf/runtime-test.env | 固定为 mysql；Docker 入口脚本会在该环境变量缺失时使用 sqlite |
| dataSourceName | conf/runtime-test.env | casdoor_test:密码@tcp(数据库地址:3306)/，末尾保留 / |
| dbName | conf/app-test.conf | 独立测试库，默认 casdoor_test，不要指向生产库 |
| origin | conf/app-test.conf | 浏览器实际访问的完整 origin，例如 https://casdoor-test.example.com 或 http://192.0.2.10:18000 |
| originFrontend | conf/app-test.conf | 本方案与 origin 一致，不加结尾 / 或路径 |
| redisEndpoint | conf/runtime-test.env | Redis 地址、连接池、密码和 DB；不用 Redis 时显式设为空值 |
| radiusSecret | conf/runtime-test.env | 当前程序仍启动 RADIUS，填写独立强密钥；不用时也不要保留空密钥 |

模板中的域名和 REPLACE_* 都是占位值，必须替换。容器里的 localhost 指容器自身，不是数据库机器或宿主机。
MySQL DSN 不要重复加库名，也不要追加 ?charset 等查询参数：当前代码直接拼接 dataSourceName + dbName。
环境变量名区分大小写，应写 dataSourceName，而非 DATA_SOURCE_NAME。
使用 Dockerfile_smy 构建的镜像已通过 CASDOOR_DB_FROM_CONFIG=true 让入口脚本信任打包的 conf/app.conf（MySQL）；注入 driverName/dataSourceName 环境变量仍可覆盖。仅在使用共享 docker-entrypoint.sh 且未设置该变量的其他构建中，缺省才会回落到 SQLite。
Docker --env-file 不解析外层引号，值不要加引号；这个 env 文件不要直接 source 到 Shell。

### 数据库与 Redis

- MySQL：仓库基准 8.0，未固定补丁版；选用你们批准的安全补丁版本，数据库字符集 utf8mb4。
- 先创建测试数据库和专用账号。应用默认不创建数据库，但启动会自动同步表结构，需要建表、建索引和改表权限；升级前备份。
- Redis：推荐 7.x，代码使用 GETDEL，最低 6.2；主要存 Session 和认证临时状态，不是通用数据库查询缓存。
- 单节点格式：HOST:6379,100,PASSWORD,0；无密码：HOST:6379,100,,0。密码不能含逗号。
- 多副本必须配共享 Redis；测试与其他 Casdoor 环境隔离。Cluster 格式：HOST1:6379;HOST2:6379,100,PASSWORD，只能使用 DB 0，不同环境不要共用集群数据空间。
- 当前格式没有 ACL 用户名、Sentinel/TLS 的显式参数，准备 Redis 服务时确认兼容性。
- 不用 Redis 时把 env 中该行改成 redisEndpoint=；Session 存工作目录 tmp/，部分认证状态存进程内存，重建/重启可能需要重新登录，仅用于单实例测试。

### 按需修改

- httpport：内部默认 8000。只改变容器外部映射端口时，不用改内部端口，但 origin 两项应填写外部地址。
- runmode：模板设为 prod，是“部署运行模式”，不是“连接生产环境”。此模式不提供开发 Swagger 静态页面；确需联调时可临时改成 dev。
- socks5Proxy：已清空，不依赖开发机的 127.0.0.1:10808；需要代理时再配置容器可达地址。
- logConfig：使用控制台日志，交给平台采集；showSql=false，避免敏感 SQL 进入日志。
- initDataFile：已清空，不自动导入仓库数据。需要导入时挂载脱敏测试 JSON，填写容器内路径，不直接复制生产数据。
- staticBaseUrl：保留默认 CDN。封闭网络应检查页面实际资源 URL 是否可达，必要时迁移资源。
- LDAP/LDAPS：端口已设为 0，不启动服务。当前 RADIUS 实现仍启动 UDP 1812，须注入独立强 radiusSecret；普通 Web 测试不要映射此端口，并限制容器网络访问。
- 邮件、短信、对象存储和第三方登录：在管理页面配置专用测试 Provider，不复用生产凭据。

### 定制会员与萨摩耶联登

仅测试这些功能时，在 env 模板中取消注释并填写：

| 变量 | 用途 |
| --- | --- |
| CASDOOR_MEMBERSHIP_CODE_PEPPER | 兑换码密钥，至少 16 字符；生成兑换码后保持稳定并备份 |
| CASDOOR_MEMBERSHIP_SERVICE_TOKEN | 会员权益服务接口访问令牌 |
| SMY_MEMBERSHIP_MERCHANT_NO | 测试商户号 |
| SMY_MEMBERSHIP_PUBLIC_KEY | 萨摩耶测试 RSA 公钥 |
| SMY_MEMBERSHIP_PRIVATE_KEY | 己方测试 RSA 私钥 |
| SMY_UNION_LOGIN_BASE_URL | 与 Casdoor 测试 origin 一致 |
| SMY_UNION_LOGIN_LOBEHUB_URL | LobeHub 测试入口，必须以 /signin/smy 结尾 |
| SMY_UNION_LOGIN_APPLICATION | 已有应用 ID，例如 admin/lobehub |

RSA 密钥使用单行 Base64 DER，或包含字面量 \n 的单行 PEM，不要直接放多行 PEM。
联登还需准备 beibei Organization、启用的 beibei-pro Plan，以及组织为 beibei 的 admin/lobehub Application。
配置 LobeHub 测试 Client ID/Secret 和精确 OIDC 回调，例如 https://lobehub-test.example.com/api/auth/oauth2/callback/casdoor；同步 LobeHub 的 APP_URL / issuer。
浏览器和服务端都应能访问测试域名；上述数据及 Provider 不由这些模板自动创建。

## 4. 组装容器运行目录

完成构建后，在项目根目录执行，先编辑模板再组装：

    cp conf/runtime-test.env.example conf/runtime-test.env
    # 编辑 conf/app-test.conf 和 conf/runtime-test.env，替换占位值。
    chmod 600 conf/runtime-test.env

    mkdir -p bin/casdoor-test/conf bin/casdoor-test/web
    mkdir -p bin/casdoor-test/files bin/casdoor-test/tmp bin/casdoor-test/logs
    cp bin/server bin/casdoor-test/server
    cp conf/app-test.conf bin/casdoor-test/conf/app.conf
    cp -R web/build bin/casdoor-test/web/
    cp -R swagger bin/casdoor-test/

复制命令适用于新建打包目录。不要在已有 web/build 或 swagger 的产物目录反复叠加复制；重新发布使用新的干净目录，避免旧资源残留。

容器中的对应目录：

    /app/                         # WORKDIR，必须在这里启动
      server                      # Linux 可执行文件
      conf/app.conf               # 测试模板的部署副本，不是开发配置
      web/build/                  # 完整前端目录，包含 index.html 和 assets
      swagger/                    # 可选，runmode=dev 时提供静态文档
      files/                      # 本地上传文件，可写并持久化
      tmp/                        # 无 Redis 时的本地 Session，可写
      logs/                       # 文件日志模式下需可写

真实 runtime-test.env 由平台作为配置/密钥注入，不放入公开构建产物或镜像。
如果已有镜像的工作目录是根目录（如仓库 STANDARD 镜像），对应路径为 /conf/app.conf、/web/build、/files，不必强制改成 /app。

**务必让测试配置成为默认的 conf/app.conf，再启动程序。** 虽然支持 --config，但 Session 和部分包级配置在该参数处理前就已读取，仅传 --config=conf/app-test.conf 可能混用开发配置。
本方案不修改程序代码，通过部署时替换默认配置规避此问题。

## 5. 启动与检查

把上述产物放入通用 Linux 容器 /app，并通过平台注入 env 中的变量：

    cd /app
    ./server

容器平台设置：

- 工作目录：/app，或实际放置上述目录的位置。
- 启动命令：./server，前台运行，不使用 nohup 或 &。
- 容器端口：TCP 8000。
- 配置挂载：测试配置 -> /app/conf/app.conf，只读。
- 环境变量：导入 runtime-test.env 的实际值；仅挂载该 env 文件不会自动加载。原始 Docker 可用 --env-file /实际路径/runtime-test.env。
- 数据卷：本地上传对应 /app/files；没有 Redis 时按需持久化 /app/tmp。
- 权限：运行用户须能执行 server、读取配置及前端文件、写入上述数据目录。
- 运行基础环境：安装 CA 根证书，按需提供时区数据。CGO_ENABLED=0 的程序不要求 Go 或系统 libc，但第三方 HTTPS 仍需要 CA。
- HTTPS：可由平台入口终止 TLS，内部 HTTP 8000；正确转发 Host 和 X-Forwarded-*，配置填写对外 HTTPS 地址。

启动健康检查（为首次建表留足时间）：

    curl -fsS http://127.0.0.1:8000/api/health

还需从对外入口实际测试页面、登录、退出、OIDC 回调。首次启动后完成管理员初始化并设置独立强密码，再开放给其他测试人员。
本地上传文件需持久化；多副本还应使用共享存储或对象存储。
