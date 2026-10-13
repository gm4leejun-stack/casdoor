# 会员权益查询接口

本文说明 BeiBei AI Casdoor 为 LobeHub、Gundam 等业务系统提供的只读会员权益查询接口。该接口是本项目的定制服务间接口，不是 Casdoor 上游的通用公开接口。

## 接口

```http
GET /api/get-membership-entitlement?issuer=<issuer>&userId=<casdoor-user-id>
X-Membership-Service-Token: <service-token>
```

请求参数：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `issuer` | 是 | Casdoor 实例地址，去掉末尾 `/` 后必须与服务端 `origin` 配置一致 |
| `userId` | 是 | Casdoor OIDC `sub` 对应的稳定用户 ID |
| `owner` | 否 | 兼容旧调用方，可继续传递，但服务端忽略该值 |
| `plan` | 否 | 兼容旧调用方，可继续传递，但服务端忽略该值 |

请求头 `X-Membership-Service-Token` 必须等于 Casdoor 服务端环境变量 `CASDOOR_MEMBERSHIP_SERVICE_TOKEN`。该令牌仅用于服务间调用，不应发送到浏览器或记录到日志。

## 固定会员范围

接口不允许调用方选择组织或套餐，始终查询：

```text
owner: beibei
plan:  beibei-pro
```

例如，旧调用方仍然请求：

```http
GET /api/get-membership-entitlement?owner=gundam&plan=gundam-pro&issuer=<issuer>&userId=<user-id>
```

服务端也只会查询 `beibei/beibei-pro`。`owner` 和 `plan` 的传入值不会改变查询范围。新调用方应省略这两个兼容参数。

## 查询与状态判定

1. 使用固定 `owner=beibei` 和 `userId` 查找 Casdoor 用户；目标组织中没有该用户时成功返回 `Missing`。
2. 使用用户内部名称查询固定 `plan=beibei-pro` 的 Subscription。
3. 根据 Subscription 的状态、开始时间和结束时间返回权益状态。有效区间采用左闭右开规则：`startsAt <= now < expiresAt`。
4. 同一用户若匹配到多条 `beibei-pro` Subscription，接口不会自行合并或选择记录，而是返回权益不可用。

成功响应由 Casdoor 的统一响应结构包装，`data` 示例：

```json
{
  "status": "Active",
  "plan": "beibei-pro",
  "startsAt": "2026-10-01T00:00:00Z",
  "expiresAt": "2026-11-01T00:00:00Z",
  "subscriptionId": "beibei/subscription-id",
  "evaluatedAt": "2026-10-13T00:00:00Z",
  "subject": {
    "issuer": "https://id.example.com",
    "userId": "<casdoor-user-id>"
  }
}
```

可能的权益状态包括 `Active`、`Upcoming`、`Expired`、`Suspended`、`Inactive`、`Missing` 和 `Error`。固定组织中没有对应用户，或该用户没有匹配的 `beibei-pro` 订阅时，都返回 `Missing`，不是接口错误。

## 排障

- 返回未授权：检查服务端是否设置 `CASDOOR_MEMBERSHIP_SERVICE_TOKEN`，以及请求头是否完全一致。不要在排障记录中输出令牌。
- 提示参数或 issuer 错误：确认 `issuer` 与 Casdoor `origin` 指向同一实例，允许的差异仅为末尾 `/`。
- 返回权益不可用：检查数据库访问是否失败，或该用户是否存在多条 `beibei-pro` Subscription 等数据冲突。固定组织中不存在该 `userId` 不属于接口错误。
- 返回 `Missing` 或非 Active：只检查 `beibei/beibei-pro` 的订阅状态和起止时间；其他 owner 或 Plan 不参与会员判定。

该接口只负责查询，不创建、续期或修改 Subscription。

## 后续会员维护接口约束

萨摩耶会员维护接口 `POST /smy/create/rightCard` 已实现，固定维护 `owner=beibei`、`plan=beibei-pro`，不允许外部请求选择 owner 或 Plan。开通、退款、幂等及客服操作档见 [维护接口说明](../smy-membership-api.md)。消费端缓存不在本次维护范围。
