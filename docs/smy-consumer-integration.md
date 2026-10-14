# BeiBei AI 接口联调说明（供萨摩耶消费方）

## 1. 调用配置

- 商户号：**BSAI**。
- 认证：服务端报文签名，不需要 Casdoor 用户密码、OAuth client secret 或会员查询令牌。
- POST，Content-Type: application/json，UTF-8，请求最大 64KiB。
- 支持版本：2.0（明文业务报文）、3.1（加密业务报文）。
- 私钥通过双方约定的安全渠道交付，仅放在服务端，不提供给浏览器或写入日志。

| 接口 | 方法 | 路径 | 内网联调地址 |
| --- | --- | --- | --- |
| 联登 | POST | /smy/unionLogin | http://192.168.12.18:18000/smy/unionLogin |
| 会员开通／退款 | POST | /smy/create/rightCard | http://192.168.12.18:18000/smy/create/rightCard |

本轮直接使用上述内网 IP，LobeHub 地址为 http://192.168.12.18:24491。消费方服务端及用户浏览器均须能访问该内网 IP 的 18000／24491 端口，无需与服务部署在同一台机器。套餐、回调及跳转配置均由我方负责，消费方不需要管理 Casdoor 组织或套餐。HTTP 仅用于可信内网开发联调，生产环境使用 HTTPS；返回的 h5Url 也使用内网地址，不使用 localhost。

双方确认本轮联调使用同一套 RSA 公私钥，公钥（Base64 DER）如下，私钥不在本文展示：

```text
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQCHG2gZ0dWW01yEHynrS1amktztIWxUOaIaFF7ygBklRfKRmVGR0WgKDM5raEdj4BLeq+V8TEk0tWPkT18IGyI1UVovHzKbvoqYtCmMD6LTTxfz1BgLsQb6YYkGZqGLGEBPt1/xhcoP89rb0+9yxTxIitYIphhNrfp8KhnMxQ03dwIDAQAB
```

## 2. 公共请求报文

所有 envelope 字段均为字符串；biz_content 是 JSON 字符串，不是 JSON 对象。业务对象内部字段按各接口要求。

| 字段 | 要求 |
| --- | --- |
| version | 2.0 或 3.1 |
| req_seq_number | 1–32 位字母／数字，建议每次请求使用新值，不含横线 |
| merchantNo | BSAI |
| timestamp | 北京时间 yyyy-MM-dd HH:mm:ss；联登前后偏差在 5 分钟内 |
| biz_content | 2.0 为原始业务 JSON 字符串；3.1 为加密后 Base64 字符串 |
| sign | 按第 5 节计算的 Base64 签名 |
| secretKey | 仅 3.1，RSA 加密 AES 密钥后的 Base64；2.0 不传 |

2.0 联登请求示例（时间必须换成请求时刻，sign 必须真实计算）：

```json
{
  "version": "2.0",
  "req_seq_number": "UL202610130001",
  "merchantNo": "BSAI",
  "timestamp": "2026-10-13 19:00:00",
  "biz_content": "{\"uniqueId\":\"SMYTEST001\"}",
  "sign": "<真实签名>"
}
```

## 3. 联登接口

POST /smy/unionLogin。业务最小内容：

```json
{"uniqueId":"SMYTEST001"}
```

- uniqueId 必填，字符串，最多 100 字节且无首尾空白；与会员接口使用同一客户标识。
- userMd5Info／mobile 可省略，传入也不匹配、不判断、不更新联系方式；验签仍覆盖原报文。
- 不需要 orderNo、productType 或 periodTime，不检查会员、订单、退款状态。非会员也能联登。
- 已绑定则复用账号，未绑定自动创建普通账号，无密码、邮箱、手机号。不按手机号合并。
- 同商户联登请求号不能重复签发链接，过期或已消费后也不能复用。重新发起须使用新请求号。

验签／解密后的响应业务内容：

```json
{"code":"000000","msg":"Success","data":{"h5Url":"<一次性联登链接>"}}
```

消费方服务端验签／解密后，将 data.h5Url 交给自己的前端，前端立即使用 window.location.assign(h5Url) 或同等浏览器导航。无需用户再点击，不用 fetch 或后端 GET 提前访问此链接；链接会被一次性消费。不要自行拼接、替换主机或缓存／记录链接。

h5Url 签发后 5 分钟有效，成功兑换一次后失效。跳转自动进入对应账号的 LobeHub 使用页面；链路失败时从消费方重新发起联登。不要求消费方调用 OAuth 回调或持有 LobeHub Cookie。

| code | 含义 |
| --- | --- |
| 000000 | 成功，返回 data.h5Url |
| 000001 | 签名错误 |
| 000017 | 参数或时间错误 |
| 000018 | 解密失败 |
| 000015 | 请求号已使用，换新请求号重新发起 |
| 999999 | 账号、配置、数据库等处理失败，按 msg 排查 |

## 4. 会员开通／退款接口

POST /smy/create/rightCard。开通业务内容：

```json
{"orderNo":"SMYORDER0001","productType":"YHK","productCode":"YHK01","uniqueId":"SMYTEST001","periodTime":"3","mobile":"13800000001","orderStatus":"2"}
```

| 字段 | 要求 |
| --- | --- |
| orderNo | 必填、全局唯一订单号，最多 100 字节、无首尾空白 |
| uniqueId | 必填、客户唯一标识，最多 100 字节、无首尾空白 |
| productType / productCode | 必填、各最多 100 字节、无首尾空白；示例值不是强制指定商品 |
| periodTime | 开通必填字符串 1／3／6／12，按相应自然月发放；退款可省略 |
| mobile | 必填、中国大陆手机号，仅留档，不用于匹配账号 |
| orderStatus | 字符串 2 开通／6 退款 |

退款业务内容：

```json
{"orderNo":"SMYORDER0001","productType":"YHK","productCode":"YHK01","uniqueId":"SMYTEST001","mobile":"13800000001","orderStatus":"6"}
```

- 不传 owner／plan，固定维护 beibei/beibei-pro。
- 未到期从原到期时间续；已过期从处理时刻续。periodTime=3 即增加 3 个自然月。
- 退款只使用原发放订单保存的月份：未开始取消全部排期，生效中保留已用时间并在处理时刻终止，已结束只记退款；后续有效订单按各自完整月份重排。
- 重复开通／退款成功，不再次续期或扣减；已退款订单的迟到开通不能恢复会员。
- 同订单 uniqueId／归属／商品参数必须与原记录一致；重复开通周期须一致，手机号可以变化。
- 重试保持同一 orderNo，不要为重试另造订单；退款无需提供新的 orderNo。
- timestamp 校验格式，不限制延迟退款通知年龄。

成功业务响应：

```json
{"code":"10000","msg":"Success","data":{"status":"SUCCESS"}}
```

失败为 code=40000、data.status=FAILED，msg 提供原因。HTTP200 不等于业务成功。账实异常由我方排查，失败事务全部回滚，不自动猜测修复。

## 5. 签名与加解密（两接口共用）

签名：
1. 对 envelope 中除 sign 外的所有非空字段按 ASCII 字段名升序排列，拼接 k=v&k=v；不 URL 编码。biz_content 使用发送时的原始字符串，不重排内部 JSON。
2. 对拼接字符串的 UTF-8 字节计算 MD5，得到 32 位小写 hex 文本。
3. 对该 hex 文本字节使用 RSA PKCS#1 v1.5 type1 私钥运算，再 Base64 得到 sign。**不是标准 MD5withRSA；不附加 DigestInfo。**
4. 接收方恢复签名内容并比对相同规则计算出的 MD5 hex。不能用重新序列化后的业务 JSON 验签。

3.1 加密：随机生成 16 字节 AES 密钥，AES-128/CBC/NoPadding；零字节补齐至 16 字节倍数，IV 与 AES 密钥相同。biz_content 为密文 Base64；secretKey 为接收方 RSA 公钥 PKCS#1 v1.5 type2 加密 AES 密钥后的 Base64。先加密再对 envelope 签名；接收方先验签再解密。2.0 不加密业务字段。

响应 envelope（2.0）：

```json
{"biz_content":"<业务响应 JSON 字符串>","sign":"<响应签名>"}
```

3.1 响应另含 secretKey，biz_content 为密文 Base64。响应没有请求的 version／merchantNo／timestamp 字段，按实际返回字段验签，按请求版本解密，不自行补字段参与验签。

正常业务响应 HTTP200；超过限制 HTTP413；协议配置或响应准备失败 HTTP503（可能为普通未签名错误 JSON），此时按失败处理。

## 6. 消费方联调步骤

1. 配置我方提供的访问基址、BSAI、约定密钥，先使用 2.0 验证签名，再验证 3.1 加密。
2. 使用独立测试 uniqueId 发起联登，前端自动导航 h5Url，核对对应 LobeHub 账号且不出现重新登录／首次引导。
3. 同 uniqueId 开通 periodTime=3 订单；先联登／先开通均应复用账号。用同订单重试，确认仅发放一次。
4. 用同 orderNo 发退款，省略 periodTime；退款重试不重复扣减。测试身份及订单须经双方确认，不对真实客户做试验。
5. 验证签名篡改、过期时间、联登请求号重复、链接重复访问的失败处理。
6. 失败提供接口、req_seq_number、orderNo（会员）、uniqueId、时间、HTTP状态和 code/msg；不要发送私钥、Cookie、长期令牌或完整 h5Url。

联登返回码与会员返回码不同，须分开判断。联登成功不代表会员有效；订单响应成功表示 Casdoor 已提交，LobeHub 会员显示刷新沿用消费端现有缓存机制。
