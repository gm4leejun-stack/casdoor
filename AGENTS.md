# Repository Guidelines

## 项目结构

- Go 后端：`main.go`、`controllers/`、`object/`、`routers/`、`conf/`。
- React/TypeScript 前端：`web/src/`；Cypress 测试：`web/cypress/`。
- Helm 部署：`manifests/casdoor/`；文档：`docs/`。
- `web-old/` 仅作参考，默认不要修改。

## 构建与测试

- `make run`：格式化、执行 `go vet` 并启动后端。
- `go test ./...`：运行 Go 测试。
- `make lint`：运行 Go linter。
- `cd web && yarn install`：安装前端依赖。
- `cd web && yarn typecheck && yarn lint && yarn build`：检查并构建前端。
- `make deps`：启动本地数据库依赖。

## 代码规范

Go 使用 `gofmt`；导出标识符使用 PascalCase，内部标识符使用 camelCase。前端遵循现有 TypeScript、React 和 ESLint 约定。保持改动聚焦，避免无关重构和大范围格式化。

## 测试规范

Go 测试文件使用 `_test.go`，测试函数使用 `Test<行为>` 命名。修改后先运行受影响模块的最小测试，再运行必要的完整检查。除非用户明确要求，不新增 Go 测试文件。

## 提交与 PR

提交信息使用 Conventional Commits，例如 `feat: ...`、`fix(auth): ...`、`docs: ...`。PR 应说明改动内容、验证命令及配置影响；UI 改动附截图。不要提交密钥、令牌或生产配置。

## Agent 约束

开始前阅读相关目录和 `CLAUDE.md`。保留用户已有改动，不执行破坏性 Git 操作；完成时报告实际运行过的验证命令。
