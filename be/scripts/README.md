# scripts

该目录存放后端开发和构建辅助脚本。脚本不会被 `go run`、后端启动流程或 Docker 构建自动执行。

## install-pdfium.ps1

用于在 Windows 本地开发环境安装 PDFium 动态库。脚本会下载 Windows x64 版本的 `pdfium.dll`，校验默认版本的 SHA-256，并安装到 `be/lib/pdfium.dll`。

在仓库根目录执行：

```powershell
.\be\scripts\install-pdfium.ps1
```

也可以指定版本：

```powershell
.\be\scripts\install-pdfium.ps1 -Version 8057
```

Windows 后端在需要渲染 PDF 页面时才会加载 PDFium，并依次从 `PDFIUM_DLL` 环境变量、项目附近的 `lib/pdfium.dll` 和可执行文件目录等位置查找。没有安装 DLL 时，后端仍可启动，但 PDF 公式截图会降级为保留公式文本。

Linux 和 Docker 构建不使用该脚本。Linux PDFium 由 `be/Dockerfile` 在镜像构建阶段下载并静态链接。
