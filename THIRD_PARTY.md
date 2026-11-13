# 来源与复用说明

- 核心时间轴数据、解析/计划、校验、预览、确认、版本防护、记录、撤销/重做及本地页面为本任务实现，没有以现成剪辑器或 Agent 完整应用为主体。
- Go 运行时仅使用标准库；没有第三方 Go 模块。Go 来源为 https://go.dev/ ，许可为 BSD 3-Clause。
- 页面使用原生 HTML/CSS/JavaScript，不加载 CDN、第三方字体或前端运行库。
- `web/demo-cover.svg` 为本任务原创的虚构静态封面，不含第三方素材，用于验收演示。
- Node.js 内置测试运行器、Playwright CLI、Microsoft Edge、ESLint（MIT）、@eslint/js（MIT）、globals（MIT）和 Prettier（MIT）仅用于开发验证，不随程序运行。开发工具版本及完整性由 package-lock.json 锁定。未复制这些项目源码作为业务实现。
- GitHub Actions 的 checkout、setup-go、setup-node、upload-artifact 是构建工具，通过工作流引用；其各自来源为 GitHub 的 actions 组织，许可证可查对应上游仓库。
- 项目整体许可证见根目录 `LICENSE`（MIT）。如交付方要求内部专有，请整体替换该文件并在提交说明中注明，不要只改 `THIRD_PARTY.md`。
