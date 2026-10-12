# 基于现有 Gundam-Agent 对接新增 LobeHub Application

状态：最小增量设计基线

版本：v2

本文只定义 Casdoor 项目的配置边界。LobeHub 的本地用户映射、伪邮箱和显示规则位于
LobeHub 项目的 `docs/development/auth/casdoor-shared-identity.md`。

## 1. 现有基线

Casdoor 与 Gundam-Agent 已完成对接，本次直接复用，不重新设计：

- 继续使用现有 Casdoor 服务、组织和用户目录。
- 继续保留 Gundam-Agent Application 的 client ID、client secret、回调地址和 Claims 配置。
- Casdoor 继续使用 `user.Id` 作为 OIDC `sub`。
- Gundam-Agent 继续按 `(issuer, sub)` 映射本地用户。
- 现有 Gundam-Agent 登录、会员接口和 Casdoor 扩展均不修改。

本次 Casdoor 的唯一新增工作是创建一个独立的 LobeHub Application。

## 2. 最小配置

```text
一套现有 Casdoor 服务和用户目录
├── 现有 Gundam-Agent Application（保持原样）
└── 新增 LobeHub Application（新增凭据和回调）
```

新建 LobeHub Application 时：

1. 选择 Gundam-Agent 当前使用的同一组织和用户目录。
2. 生成独立的 client ID 和 client secret。
3. 精确配置 LobeHub 回调地址，例如
   `https://lobe.example.com/api/auth/callback/casdoor`。
4. 启用 LobeHub 现有 provider 所需的 Authorization Code 和 OIDC scopes。
5. 保留现有稳定 `sub`，并提供标准 profile 中的显示名称和头像（存在时）。

Gundam-Agent Application 不需要重新创建，也不需要增加 LobeHub 回调地址。

## 3. 两个平台共享与不共享的内容

共享的只有 Casdoor 身份事实：

| 字段  | 约定                                     |
| ----- | ---------------------------------------- |
| `iss` | 同一个 Casdoor issuer                    |
| `sub` | 同一用户保持相同，继续使用现有 `user.Id` |

每个 Application 独立的内容：

- `client_id`、`client_secret`、redirect URI 和 `aud`。
- Application 级的 TokenFields、权限和停用操作。
- 下游平台如何保存兼容字段、显示用户资料和管理业务权限。

因此不要求 Gundam-Agent 与 LobeHub 消费完全相同的五字段 Claims。Gundam-Agent 沿用现有
Claims；LobeHub 使用 `sub`、显示名称和头像，并在 LobeHub 本地处理邮箱兼容。

## 4. 无邮箱用户

Casdoor 不需要为 Gundam-Agent 改写邮箱，也不需要建设 Claims 桥接服务。

LobeHub 因本地用户模型需要非空邮箱，会在自己的 Casdoor provider 中根据 `sub` 生成：

```text
casdoor-<base64url-no-padding(UTF-8(sub))>@id.invalid
```

无论用户是否有真实邮箱，LobeHub 都使用该内部兼容值。该值不写回 Casdoor 用户表，不作为
真实联系方式，也不会影响 Casdoor 原有的邮箱验证、通知或账号恢复。Casdoor 只需确保 `sub`
稳定、非空。

## 5. 登录方式兼容

手机号、密码、短信验证码、微信、GitHub 等方式仍由 Casdoor 管理。增加或调整登录方式时，
只要它们绑定到现有 Casdoor 用户并签发同一个 `sub`，LobeHub 和 Gundam-Agent 都不需要因此
修改身份映射。

不能按昵称、未验证邮箱或未验证手机号自动合并用户；无法确认是同一用户时保持为不同的
Casdoor 用户。

## 6. 个性化边界

不同平台确有差异时，优先使用各自 Casdoor Application 的配置隔离，例如回调地址、scope、
TokenFields 和密钥。这属于 Casdoor 原生的 Application 级配置，不是新建一套服务或独立桥接。

当前 LobeHub 方案不需要新增个性化桥接代码：Casdoor 新建 Application，LobeHub 自己处理其
邮箱兼容和界面显示即可。

## 7. 验收清单

- [ ] Casdoor 仍只运行现有一套服务并使用现有用户目录。
- [ ] Gundam-Agent Application 的配置和 Claims 未修改，现有登录回归正常。
- [ ] 新建 LobeHub Application，凭据和回调地址与 Gundam-Agent 相互独立。
- [ ] 同一测试用户从两个 Application 登录时，`iss` 和 `sub` 相同，`aud` 分别对应各自
      client ID。
- [ ] 无邮箱用户可以通过 LobeHub Application 完成 OIDC 登录；邮箱兼容由 LobeHub 本地完成。
- [ ] 密码、短信验证码和已绑定第三方登录对同一用户签发相同 `sub`。
- [ ] 禁用或删除用户后，不能再通过任一 Application 发起新的有效登录。

## 8. 明确不做

- 不修改或迁移现有 Gundam-Agent Application。
- 不修改 Gundam-Agent 代码、用户映射、Claims 或会员逻辑。
- 不要求 Gundam-Agent 使用 LobeHub 的伪邮箱规则。
- 不复制 Casdoor 用户数据，不部署第二套 Casdoor。
- 不建设身份桥接服务、用户同步、消息队列或统一会员中心。
- 不把 LobeHub 或 Gundam-Agent 的业务数据写入 Casdoor 用户对象。
