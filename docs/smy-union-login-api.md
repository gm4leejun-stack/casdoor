# 萨摩耶联登接口

## 范围与身份

`POST /smy/unionLogin` 由萨摩耶服务端调用，返回 `h5Url`，由萨摩耶前端自动跳转，无需用户再点击。联登只认证身份，不查询订单、套餐、会员有效期或退款，不修改会员。文档残留订单校验不适用。

- 固定组织 `beibei`，按萨摩耶 `uniqueId` 绑定定位用户。Casdoor 内部 `User.Id` 独立、稳定，继续作为 OIDC 身份。
- 已绑定复用原账号；未绑定创建自动用户名的 `normal-user`，无密码、邮箱、手机号。不按手机号匹配或合并账号。
- `userMd5Info` 与 `userMd5Info.mobile` 均可省略；传入也不判断、不匹配、不更新。已有联系方式保持原样。
- 禁用、删除、绑定不一致的账号拒绝登录，不创建替代账号。额外安全验证及权限限制不绕过。
- 联登创建的绑定 sequence 为 0，不建立 Subscription 或会员操作档；首次会员维护才初始化，并按同一 uniqueId 复用账号。

## 请求与响应

Content-Type: `application/json`，最大 64KiB。公共 envelope 同会员接口：`version`（2.0/3.1）、`req_seq_number`、`merchantNo`、`timestamp`、`sign`、`biz_content`；3.1 另带 `secretKey`，字段值为字符串。

业务最小请求：

```json
{"uniqueId":"customer001"}
```

也兼容 `{"uniqueId":"customer001","userMd5Info":{"mobile":"13800000001"}}`。手机号虽忽略，仍须按原报文完整签名。

`uniqueId` 非空字符串、最多 100 字节、无首尾空白；请求号为 1–32 位字母数字。timestamp 为北京时间 `yyyy-MM-dd HH:mm:ss`，与服务端时间偏差须在 5 分钟内。每次重新发起用新请求号：同商户已用请求号不能再次签发，包括过期、已兑换请求。这项时间限制只用于联登，不改变会员维护的延迟退款规则。

2.0 明文，3.1 RSA 加密 AES 密钥并加密业务字段；签名为非空字段 ASCII 排序、MD5 小写 hex 字节及 RSA PKCS1 type1，保留 biz_content 原字符串，AES128 CBC 零补齐、IV=key。详细协议复用 [会员接口说明](smy-membership-api.md)，不是标准 MD5withRSA。

响应 envelope 保持协议签名／3.1 加密。解开 biz_content 后：

```json
{
  "code":"000000",
  "msg":"Success",
  "data":{"h5Url":"https://casdoor.example.com/smy/unionLogin/exchange?ticket=<一次性凭证>"}
}
```

失败不返回 h5Url，msg 给出原因：

| code | 含义 |
| --- | --- |
| 000000 | 成功 |
| 000001 | 签名错误 |
| 000017 | 参数或时间错误 |
| 000018 | 解密失败 |
| 000015 | 请求号已使用 |
| 999999 | 配置、账号、数据库等处理失败，详见 msg |

联登采用正式返回码，不顺带修改已有会员维护返回码。过大请求 HTTP413；协议配置缺失或响应制作失败 HTTP503。用户、绑定及凭证在同事务创建，响应在提交前准备，失败全部回滚，不返回成功链接。

## 浏览器链路

1. 萨摩耶前端自动导航到 h5Url，不用 fetch 替代浏览器导航。
2. `GET /smy/unionLogin/exchange?ticket=...` 原子兑换，复核绑定、账号、目标应用及安全限制，建立正规 Casdoor 会话。
3. 302 到固定 LobeHub `/signin/smy?subject=<Casdoor User.Id>`。subject 仅核对身份，不是认证凭证。
4. LobeHub 清除旧会话，自动发起现有 Better Auth Casdoor OIDC，正常生成 state、PKCE 和 Cookie，不做第三方账号绑定。
5. 正常回调 `/api/auth/oauth2/callback/casdoor`，再由完成入口核对当前 Casdoor accountId 与 subject 一致；仅此链路完成首次引导标记并进入 `/`。失败停在错误页面，不回退旧账号首页。

凭证从签发起 5 分钟有效，数据库仅存摘要，成功兑换后失效；并发兑换只有一次成功。过期、已使用或链路中断时，从萨摩耶使用新请求号重新发起。链接不得分享或写入日志；应用禁止缓存和向外发送 Referer，反向代理日志也需隐藏 ticket 查询值。LobeHub 原会员消费及登录后刷新保持不变，非会员或会员过期不阻止联登。

## 配置、迁移及上线

复用 `SMY_MEMBERSHIP_MERCHANT_NO`、`SMY_MEMBERSHIP_PUBLIC_KEY`、`SMY_MEMBERSHIP_PRIVATE_KEY`。新增：

| 配置 | 要求 |
| --- | --- |
| SMY_UNION_LOGIN_BASE_URL | Casdoor 公网 origin，如 https://casdoor.example.com，无路径、查询参数 |
| SMY_UNION_LOGIN_LOBEHUB_URL | 固定 LobeHub 入口，如 https://lobe.example.com/signin/smy，无查询参数 |
| SMY_UNION_LOGIN_APPLICATION | 现有 LobeHub 应用 owner/name，如 admin/lobehub；组织为 beibei |

URL 要求 HTTPS，仅 loopback 开发允许 HTTP。应用需启用登录及 EnableAutoSignin，配置精确回调 `https://lobe.example.com/api/auth/oauth2/callback/casdoor`，openid/email/profile 不应要求授权确认，不能配置需交互的提示页；LobeHub AUTH_CASDOOR_ID 对应同一应用。不接受请求方指定跳转，不自动放宽应用安全配置。联登仅支持普通非管理员账号；已启用 MFA、要求配置 MFA、要求更新密码等的账号拒绝自动联登，不绕过安全限制。

启动会显式移除客户表单列 mobile 唯一索引，保留全部数据及其他唯一约束，再 Sync2 创建凭证表 `smy_union_login_ticket`。上线前备份数据库，先部署 LobeHub 入口，再部署 Casdoor 及配置，最后开放接口。未部署代码不会生效。多个无手机号绑定产生后不能直接恢复旧手机号索引／旧身份代码；回退优先关闭新入口，保留全部账号及会员数据。

## 排查与验证

- 接口失败：按 msg 核对商户、签名、版本、新请求号、北京时间及配置。
- 链接失败：核对到期／已消费状态、用户禁用／删除及应用安全限制，再重新发起。
- 账号问题：按 uniqueId 查绑定，核对 Casdoor User.Id 与 LobeHub Casdoor accountId，不按手机号猜测合并。
- 会员问题：按 uniqueId／订单号／内部用户 ID 查会员操作档，联登成功不代表会员有效。
- 出现登录／授权页：检查自动登录、授权配置、client ID、实际回调和跨站 Cookie，不绕过 state／PKCE。

最小回归：`go test -tags skipCi ./object ./controllers ./routers -run 'TestSmy|TestMembership' -count=1`，另运行 build/vet。数据库并发使用独立空测试库，禁止真实客户测试退款。LobeHub 自动入口、完成路由及原 Casdoor 登录回归后，还需两端部署及商户配置完成后进行真实浏览器联调。
