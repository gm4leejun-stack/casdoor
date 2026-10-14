# Samoyed SMS Provider 配置设计

## 目标

在 Casdoor 的 `Samoyed SMS` Provider 页面直接配置固定协议所需的四项参数，保存后测试和实际验证码发送立即使用，不再依赖进程环境变量，也不需要重启。

## 最小实现

复用 Provider 现有字段，避免数据库迁移：

- `endpoint`：短信接口 URL
- `clientId`：Business Code
- `clientId2`：Message Enum Code
- `clientSecret`：AES 密钥

页面仅为 `Samoyed SMS` 显示这四个明确命名的输入框。AES 密钥使用密码框，并沿用 Provider 现有的 `***` 脱敏与更新时保留旧密钥机制。

后端通过当前 Provider 构造短信客户端，校验四项非空及 AES 密钥为 16、24 或 32 个 UTF-8 字节。既有请求协议、手机号格式、加密和响应判断保持不变。删除该类型对全局 `sms*` 启动配置的依赖及 Compose 中相应变量。

## 验证

- 后端测试证明客户端读取 Provider 参数，不读取全局配置。
- 运行相关 Go 测试。
- 运行前端类型检查。
