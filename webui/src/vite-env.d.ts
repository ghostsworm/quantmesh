/// <reference types="vite/client" />

// 建構時注入的全局变量
declare const __APP_VERSION__: string
declare const __BUILD_TIME__: string
declare const __GIT_HASH__: string

interface ImportMetaEnv {
  readonly VITE_GITHUB_REPO?: string
  readonly VITE_DISABLE_OPEN_SOURCE_UPDATE?: string
}
