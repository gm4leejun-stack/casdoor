# LobeHub Casdoor Phone Signup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 以最低成本让 LobeHub 的 Casdoor 登录默认使用中国大陆手机号验证码，并支持在独立注册页完成新用户注册；保留用户名密码登录，隐藏其他登录和注册方式。

**Architecture:** 复用 Casdoor 现有 Application 配置、短信验证码安全机制、用户目录、OAuth/OIDC 回跳和登录完成流程。前端固定展示 `+86` 并提交 `countryCode: "CN"`；后端只补充中国大陆手机号约束、协议校验和“已有手机号转登录”分支，不新增基础设施、数据表或认证架构。

**Tech Stack:** Go、React、TypeScript、Casdoor OAuth/OIDC、现有短信验证码 Provider

---

## Task 1：固定 +86，并落实协议门禁

**Files:**

- Modify: `web/src/pages/auth/LoginPage.tsx`
- Modify: `web/src/pages/auth/SignupPage.tsx`
- Modify: `form/auth.go`
- Modify: `controllers/verification.go`
- Modify: `controllers/account.go`

- [x] 登录页手机号模式移除国家/地区选择器，固定显示 `+86`、固定提交 `countryCode: "CN"`；默认方式仍由 Application 的第一项 Signin Method 决定。
- [x] 注册页手机号移除国家/地区选择器，固定显示 `+86`、固定提交 `countryCode: "CN"`。协议默认不勾选；当 Agreement 为 required 且未勾选时，同时禁用获取验证码和注册提交。协议名称与链接继续读取现有 BeiBei AI 配置，不硬编码。
- [x] 在 `AuthForm` 增加 `Agreement bool json:"agreement"`，并在后端注册入口再次校验 required Agreement，不能只依赖前端。
- [x] 后端仅对本次 signup phone 和 phone-only login 流程拒绝非 `CN` 国家码；不要改变找回密码、MFA、资料修改等其他验证码流程。
- [x] 不改 `SendCodeInput` 公共组件；直接使用它现有的 `disabled` 行为。

**Verify:**

- [x] Run: `go test ./controllers ./form`
- [ ] Run: `cd web && yarn typecheck && yarn lint && yarn build`
- [ ] Manual: 登录和注册手机号前缀固定为 `+86`；注册未勾选协议时不能发码或提交；篡改请求为非 `CN` 时后端拒绝。

## Task 2：注册页已有手机号转为登录

**Files:**

- Modify: `controllers/verification.go`
- Modify: `controllers/account.go`
- Modify only if required for minimal reuse: `controllers/auth.go`

- [x] Signup 手机发码阶段移除 `Phone already exists` 枚举：已有正常、禁用或删除账号均不在发码前暴露账号状态，并继续沿用现有 CAPTCHA、重发间隔、验证码有效期和失败冻结。
- [x] Signup 验证码通过后、创建用户前按规范化手机号查询用户：正常已有用户直接登录原账号并继续原 OAuth/OIDC；禁用或删除用户统一返回“账号暂不可用，请联系客服”，且不创建重复账号；不存在时继续原注册创建流程。
- [x] 优先复用 `HandleLoggedIn()` 或 Signup 现有收尾逻辑完成应用限制检查、session、consent 和 OAuth code；不要复制或重构认证流程。
- [x] 新用户继续使用 `GenerateIdForNewUser()` 生成 Username，`DisplayName` 设为 `用户 ` 加 `util.GetMaskedPhone(phone)`；OIDC `sub` 继续使用 Casdoor 稳定用户 ID。
- [x] 不新增表、索引、迁移、事务框架、Redis、队列、设备指纹或额外风控。

**Verify:**

- [ ] Run: `go test ./controllers ./object ./util`
- [ ] Run when affordable: `go test ./...`
- [ ] Manual OAuth smoke: 新手机号注册后返回 LobeHub；已有正常手机号从注册页登录原账号并返回；禁用或删除账号验证码通过后被拒绝；验证码过期、错误冻结、重发限制和 CAPTCHA 仍按现有逻辑生效。

## Task 3：自动初始化 Application 配置并完成生产验证

**Files:**

- Modify: `object/init.go`

- [x] Casdoor 启动时幂等更新现有 `admin/lobehub` Application；仅当其 Organization 为 `beibei` 时生效，不自动创建 Application。
- [x] 自动开启注册和密码、验证码登录，Signin Methods 固定排序为：第一项 `Verification code / Phone only`，第二项 `Password / All`。
- [x] Signup Items 自动固定为 ID（隐藏、随机）、Phone（显示、必填、需要验证）、Agreement（显示、必填）、Signup button；不提供其他注册项。
- [x] 保留短信和 CAPTCHA Provider 记录，但关闭所有 Application Provider 的登录、注册展示，并关闭遗留 Google One Tap 展示。
- [x] 不覆盖 Client ID、Client Secret、redirect URI、trusted、短信 Provider、主题、BeiBei AI 品牌、用户协议或隐私政策等现有数据。
- [ ] 生产发布 Casdoor 新版本并完成一次正常启动后，配置即写入 Casdoor 数据库，无需进入管理后台人工配置，也无需额外重启。
- [ ] 发布后检查 `admin/lobehub` 的 Organization 仍为 `beibei`，并执行以下端到端验证；若 Application 不存在或 Organization 不匹配，应停止验收并修正生产基础数据。
- [x] 代码回滚不会自动恢复已写入的旧 Application 字段；若需要回退本配置，按投产前记录的原值恢复上述字段。
- [x] 保留已有会话；不修改 LobeHub，不做手机号换绑、注销或存量迁移。

**Final verification:**

- [ ] 从 LobeHub 点击登录：进入 Casdoor，默认手机号验证码，可切换用户名密码，页面无邮箱验证码、微信、GitHub 等其他方式。
- [ ] 点击“注册新用户”：独立注册页只显示固定 `+86` 手机号、验证码、协议勾选和注册按钮。
- [ ] 完成新用户、已有用户、禁用/删除用户三条 OAuth/OIDC 冒烟路径，并确认最终都符合 Task 2 结果。
- [x] Run: `git diff --check`

## 生产投产配置项

- 环境：production。
- 发布物：包含 `object/init.go` 中 LobeHub Application 自动初始化逻辑的 Casdoor 镜像或版本。
- 投产前：记录 `admin/lobehub` Application 本次涉及字段的原值；确认该 Application 已存在且 Organization 为 `beibei`；确认生产短信和 CAPTCHA Provider 已存在并关联，凭据有效。
- 生效方式：正常发布 Casdoor 并完成一次启动，启动过程自动将登录方式、注册项和 Provider 展示开关写入数据库；不需要进入管理后台人工配置，也不需要发布后额外重启。
- 验收：从 LobeHub 发起登录，确认默认手机号验证码且可切换用户名密码；注册页仅显示固定 `+86` 手机号、验证码、协议和注册按钮；验证新用户、已有用户、禁用或删除用户三条 OAuth/OIDC 路径。
- 安全边界：初始化不创建 `admin/lobehub`，不创建短信或 CAPTCHA Provider，不写入或覆盖 Provider 凭据；基础数据缺失或 Organization 不匹配时安全跳过，投产验收应停止并修正基础数据。
- 回滚：回滚代码不会自动恢复已经写入数据库的旧值；按投产前记录恢复 Application 对应字段。
