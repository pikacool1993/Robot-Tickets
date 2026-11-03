# cloud-admin-web

`cloud-server` 的管理员后台 Web 应用，使用 Vue 3、TypeScript 和 Vite。

## 本地开发

在 `apps/cloud-admin-web` 目录执行。若 PowerShell 提示找不到 `pnpm`，先启用 Node 自带的 Corepack：

```powershell
corepack enable
corepack prepare pnpm@11.25.0 --activate
Copy-Item public/runtime-config.example.js public/runtime-config.js
pnpm install --ignore-scripts
pnpm dev
```

Vite 将 `/api` 请求代理到 `http://localhost:8080`。生产构建使用 `pnpm build`，部署时由 Nginx 提供 `dist` 并将 `/api/` 转发到 `cloud-server`。

生产环境应在容器启动时根据环境变量生成同名的 `runtime-config.js`，不要在构建阶段把密钥写入静态资源。运行时传输密钥会被浏览器读取，只用于业务信封加密，不能代替管理员身份认证；不要将密钥写入 Git。
