# 萨摩耶会员维护 Implementation Plan

实施状态：已完成代码、受影响模块测试、隔离PostgreSQL并发及回滚race测试、Go build/vet、前端typecheck/针对性lint/build。未与萨摩耶正式密钥联调，未部署、推送或重启。接口文档为 docs/smy-membership-api.md。

**Goal:** 落地自然月会员维护、可重放客服操作档和事务幂等，不涉及联登。

**Architecture:** 纯账本计算与数据库事务、公共报文协议分离；客户操作档完整快照提供可重放来源，一条 Subscription 提供查询状态。新表在 Casdoor 现有 Sync2 入口同步。

**Tech Stack:** Go、Beego、Xorm、PostgreSQL/MySQL、React/TypeScript。

---

### Task 1: 账本计算
- [x] 新建 object/smy_membership_ledger.go：输入验证、月末截断、退款重排、重复/迟到终态。
- [x] 扩展 object/redemption_service_test.go：A/B/C、进行中/过期退款、连续重排、月末、重复/冲突。
- [x] 运行 go test ./object -run 'TestSmy'，全部通过。

### Task 2: 协议
- [x] 新建 controllers/smy_protocol.go：环境商户/密钥、原文签名、2.0/3.1请求及响应。
- [x] 扩展 controllers/membership_entitlement_test.go，验证篡改拒绝和加解密往返；运行 go test ./controllers -run 'TestSmy'。

### Task 3: 事务与客服API
- [x] 新建 object/smy_membership_store.go：客户绑定、订单PK、递增操作档、锁、完整重放校验、事务写入、管理员查询。
- [x] 修改 object/ormer.go 同步新表；修改 object/subscription.go 托管写入保护；新建 controllers/smy_membership.go，修改 routers/router.go 和 routers/authz_filter.go，外部请求仅验签放行，客服查询只管理员。
- [x] 扩展现有 object/redemption_postgres_test.go：隔离数据库真实事务回滚、重复及账号创建、账实错误拒绝。

### Task 4: 客服页面与文档
- [x] 新建 web/src/pages/MembershipHistoryPage.tsx，接入App路由和SubscriptionListPage入口，查询手机号/用户ID/订单号，展示前后有效期和详情。
- [x] 新建 docs/smy-membership-api.md，更新现有会员查询文档，明确配置、错误、托管写保护、缓存边界。
- [x] 运行最小Go测试、必要构建检查和前端typecheck/lint，记录实际结果。

### Task 5: Review
- [x] 审查事务无会话外写入、锁顺序、跨客户订单冲突、操作档重放和人工入口保护。
- [x] 完成验证汇总；未获新授权不推送或重启，提供变更和实际测试结果。
