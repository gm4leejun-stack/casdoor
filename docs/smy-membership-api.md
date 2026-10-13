# 萨摩耶会员维护接口

## 范围与配置

`POST /smy/create/rightCard`，固定维护 `beibei/beibei-pro`，不读取调用方的 owner/plan。本接口只负责会员维护；独立联登说明见 [smy-union-login-api.md](smy-union-login-api.md)。

环境变量（无默认密钥，缺失时 HTTP 503，不写业务数据）：
- `SMY_MEMBERSHIP_MERCHANT_NO`：约定商户号。
- `SMY_MEMBERSHIP_PUBLIC_KEY`：萨摩耶 RSA 公钥，验签及加密响应 AES 密钥。
- `SMY_MEMBERSHIP_PRIVATE_KEY`：己方 RSA 私钥，响应签名及解密请求 AES 密钥。

密钥可为 PEM（支持字面 `\n`）或 Base64 DER；公钥支持 PKIX/PKCS1，私钥支持 PKCS8/PKCS1。请由双方安全交换正式密钥，不提交到仓库。需要 PostgreSQL/MySQL 行锁；固定组织和启用套餐须已存在。上线需要构建、设置环境变量及重启。

## 请求与响应

当前版本启动时会先显式移除客户表单列 mobile 唯一索引，保留全部历史数据及其他唯一约束，再 Sync2 创建会员三表及联登凭证表。上线前备份数据库；仅有代码改动不会在线上生效。

### 账号定位

会员维护与联登共用同一份 `uniqueId` 绑定，固定组织 beibei。已绑定时复用原 Casdoor User.Id；未绑定时在同事务创建自动用户名的普通账号，不设置密码、不填邮箱或手机号，不按手机号匹配、合并或更新账号。先联登再开通、先开通再联登均使用同一账号；绑定异常、用户禁用或删除时失败，不创建替代账号。

本接口的 mobile 仍按原维护报文规范必填并校验格式，仅保留作订单输入和客服历史线索；不同通知中的手机号可变化，不影响 uniqueId 定位。旧绑定和操作档中的手机号原样保留，不重写历史数据。

Content-Type 为 application/json。请求 envelope 所有字段为字符串：`version`（2.0 明文或3.1加密）、`req_seq_number`（1–32字母数字）、`merchantNo`、`timestamp`（北京时间 yyyy-MM-dd HH:mm:ss）、`sign`、`biz_content`；3.1另外带`secretKey`。请求不超过64KiB。

biz_content业务字段：
```json
{"orderNo":"订单号","productType":"YHK","productCode":"YHK01","uniqueId":"萨摩耶用户标识","periodTime":"1","mobile":"13800000001","orderStatus":"2"}
```
`orderStatus=2`开通、`6`退款。开通的`periodTime`按规范支持`1`、`3`、`6`、`12`，分别发放相应数量的自然月；其他值返回失败。退款不读取通知的`periodTime`计算或校验周期，可省略或与开通不同，以已发放订单记录为准。账号按上述 uniqueId 规则定位。商品字段留档，不用于选择其他套餐。uniqueId、订单归属及商品参数必须与首次通知一致；手机号变化不拒绝请求，重复开通的periodTime必须与原发放订单一致。

签名：除sign外非空参数按ASCII字段名排序，`k=v&...`；保留biz_content原字符串。MD5计算32位小写hex，再RSA PKCS1 type1私钥运算、Base64，**不是标准MD5withRSA**。3.1先AES128/CBC/NoPadding（零补齐），IV等于16字节AES密钥，再签名；secretKey为对方RSA公钥PKCS1 type2加密后的AES密钥。

超过64KiB的请求由入口过滤器返回HTTP413，不进入业务处理。正常业务响应HTTP200，envelope含biz_content/sign及3.1的secretKey。解开biz_content：
```json
{"code":"10000","msg":"Success","data":{"status":"SUCCESS"}}
```
业务/一致性失败返回code40000、status FAILED、msg明确原因；协议配置或响应制作失败返回HTTP503。成功响应在提交事务前准备；提交失败不返回SUCCESS。网络丢响应时以同订单重试。timestamp仅校验格式，不限制退款通知年龄。

## 会员与退款规则

每笔订单按`periodTime`发放对应的北京时间自然月数，UTC保存。未到期从原到期时间续，已过期从处理时刻续。一次增加订单完整月份，只在目标月月底截断：1月31日加1个月→2月28/29日，加3个月→4月30日，不逐月截断成4月28日。

- 未开始退款：取消该订单的全部未用时段，后续未退款订单按各自periodTime前移重排。
- 生效中退款：处理时刻终止，已用时间保留，后续未退款订单从该时刻按各自periodTime重排。
- 已结束退款：只记退款，不追扣其他会员。
- 退款先到：记录终态，不发放会员；迟到开通不能恢复。
- 重复开通/退款成功，但不增加时长、扣减或新增有效期变化记录。

退款只根据原订单的发放月份和当前排期判断使用情况，通知中的periodTime不覆盖原发放月份。退款先到且没有发放记录时，仅记录终态，不假设发放月份或扣减会员；迟到开通不恢复。原通知值保留在操作档输入中，客服页面分别展示“通知周期”和“原发放周期”。

例：A到11/15、B到12/15、C到1/15，取消未来B→12/15，再取消未来C→11/15。B正在生效，11/25退款：C从11/25至12/25。

多月例：A从1/15至2/15（1个月），B至5/15（3个月），C至11/15（6个月）。未来B退款后，C从2/15至8/15；若B在3/15退款，C从3/15至9/15。已用时间不追扣，后续订单仍保留各自完整月份。

## 事务及操作档

三表：`smy_membership_customer`身份绑定及递增序号、`smy_membership_order_record`当前订单状态/排期、`smy_membership_operation`追加业务操作档。首次初始化保存原会员；每条保存订单请求、处理时间、业务原因、会员及全部订单前后快照。按客户内sequence正序可重算会员生命周期。

联登只创建序号为 0 的身份绑定，不初始化会员操作档。首次会员维护仅在序号为 0 且没有操作档时初始化；异常序号、游离订单或不一致档案返回失败。只有初始化会员操作档后才纳入托管会员写保护。

所有会员写入口使用相同数据库锁次序；组织锁覆盖首次建号，再锁用户。当前为简单实现会串行固定组织的维护请求。用户/绑定创建、订单排期、操作档、序号和总Subscription在同事务；前后重放并对账，不一致失败全部回滚，不猜测修复。

接入后，通用订阅增改删和兑换码不能绕过操作档改托管会员；时间经过导致的状态刷新仍允许，且只更新状态不覆盖并发续期。人工/其他渠道维护须先接入同操作档，当前未实现人工写接口。直接SQL修改会导致后续维护失败。

## 客服查询

订阅列表→“客户会员操作档”（`/membership-history`）。

`GET /api/get-membership-customer-history?field=uniqueId|mobile|userId|orderNo&value=精确值`，仅全局管理员或beibei管理员。无分页或模糊搜索；按客户sequence正序展示，日期北京时间。返回当前Subscription、实际订单表排期、业务操作档和`consistencyError`。发现账实异常仍返回档案供客服排查，不自动修复。

优先按 uniqueId 查询。历史手机号可能对应多个客户，此时返回 candidates 供客服选择 uniqueId，不任意选取或合并账号。仅联登、尚无会员操作档的正常绑定返回 membershipInitialized=false，页面显示“尚无会员操作档”，不是一致性错误。

会员消费查询仍为`GET /api/get-membership-entitlement`，固定beibei/beibei-pro。本接口提交即更新Casdoor数据；消费端缓存刷新是其现有行为，本次不修改。

## 验证

最小测试：`go test -tags skipCi ./object ./controllers ./routers -run 'TestSmy|TestMembership|TestEvaluateMembership' -count=1`；编译`go build ./...`。真实并发/回滚测试在现有redemption_postgres_test.go，需新建空隔离数据库，名称以`smy_membership_agent_`开头，设置`CASDOOR_TEST_POSTGRES_DSN`，运行`go test -race -tags skipCi ./object -run TestSmyMembershipPostgresTransactions -count=1 -v`。不使用现有会员造测试数据。
