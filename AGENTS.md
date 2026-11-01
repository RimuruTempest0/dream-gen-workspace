# 用户本机配置与后续发布

用户已配置 Portable 0.3.0 的生图中转站和 LLM 供应商，要求后续开发版本保留，无需重复配置。

- 构建使用 `scripts/build.ps1 -Version '版本号'`，默认会根据 `.local/profile-source.txt` 沿用已配置的数据目录，并给本机输出目录写入 `data-location.txt`。不要跳过这个步骤后把空白版本作为用户本机升级版交付。
- 交付时提供已关联数据的 `release/ImageWorkbench-版本号/ImageWorkbench.exe` 本机路径。发布 ZIP 是干净包，不含用户数据与目录指针；若使用 ZIP 升级，覆盖原程序目录并保留数据。
- 用户的 LLM 供应商和生图中转站记录与 Key 只保留在其本机测试目录，绝不进入 ZIP。`build.ps1` 打包后的 `scripts/packcheck` 检查发现问题会删除 ZIP 并中止构建；不要绕过或删除此检查。改动打包内容时同步更新 `scripts/packcheck` 的允许清单。
- 不删除实际数据目录，不覆盖 `data-location.txt` 指向的另一份配置。需要调整时遵循 `docs/local-development-profile.md`。
- API Key 继续使用现有 Windows DPAPI 加密文件，不写入源码、文档、对话或发布包。不要为验证配置保留而调用真实付费生成。
- 改动本机配置保留流程后，运行 `scripts/test-local-profile.ps1`。测试与桌面 QA 必须继续使用隔离数据，避免回写用户配置。
