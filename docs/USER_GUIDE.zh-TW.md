# Artifact Downloader 使用者操作與設定手冊

本手冊適用於設定檔版本 `1`，說明如何驗證與執行下載工作、撰寫任務與環境政策 YAML，以及常見的 URL、Gradle、Maven、npm、Yarn、pip、私有 Git、Proxy、Callback 與自動化操作場景。

## 1. 工具用途與執行模型

Artifact Downloader 是 YAML 驅動的命令列工具，支援兩種 job：

- `urls`：從文字清單讀取 HTTP/HTTPS URL，並行下載檔案。
- `package`：建立暫存 workspace、clone Git repository、切換目錄，再以內建固定命令下載或建置依賴。

設定檔中的 job 依列出順序逐一執行。單一 job 失敗後，若程序未被取消，後續 job 仍會繼續；最後只要任一 job 失敗，程序即以結束碼 `1` 離開。每個 URL job 內部則會依 `concurrency` 並行下載，遇到第一個錯誤後停止派送新工作。

## 2. 安裝與執行前準備

### 2.1 建置執行檔

需要 Go 1.25 或更新版本：

```bash
go build -o artifact-downloader ./cmd/artifact-downloader
./artifact-downloader help
```

### 2.2 外部工具需求

URL job 不需要套件管理器。Package job 需要系統 `PATH` 中存在 `git` 與對應工具：

| Package manager | 必要命令 | 專案必要檔案 |
| --- | --- | --- |
| Gradle | `gradle` | 可由 `gradle build` 建置的專案 |
| Maven | `mvn` | `pom.xml` |
| npm `install` | `npm` | `package.json` 與 lockfile |
| npm `install-unlocked` | `npm` | `package.json` |
| Yarn | `yarn` | `package.json` 與相容 lockfile |
| pip | `python3` 與 pip | `requirements.txt` |

本工具只呼叫系統安裝的工具，不會使用 repository 內的 `gradlew` 或 `mvnw`。

## 3. 基本操作

### 3.1 驗證任務設定

每次執行前先驗證 YAML：

```bash
./artifact-downloader validate --config ./artifact.yaml
```

成功輸出例如：

```text
configuration is valid (2 jobs)
```

驗證採嚴格模式，拼錯或未支援的 YAML 欄位也會失敗。`validate` 只驗證任務 YAML；環境政策會在 `run --environment-config` 時載入及驗證。

### 3.2 執行全部或單一工作

```bash
# 依設定順序執行全部 job
./artifact-downloader run --config ./artifact.yaml

# 只執行指定名稱的 job
./artifact-downloader run --config ./artifact.yaml --job download-files

# 顯示 git 與套件命令的輸出
./artifact-downloader run --config ./artifact.yaml --verbose
```

路徑不受目前 shell 目錄影響：任務 YAML 內的 `urlList`、`output`、`cache` 都以該 YAML 所在目錄為基準解析。

### 3.3 除錯選項

```bash
./artifact-downloader run \
  --config ./artifact.yaml \
  --verbose \
  --keep-workspace
```

`--keep-workspace` 只影響 package job，會印出並保留暫存 workspace；其中可能包含 clone 下來的原始碼或建置資料，除錯完應自行安全移除。正常模式無論成功或失敗都會清除 workspace。

### 3.4 CLI 選項表

| 子命令／選項 | 用途 |
| --- | --- |
| `help`、`-h`、`--help` | 顯示語法 |
| `validate --config <file>` | 驗證任務設定，不執行 job |
| `run --config <file>` | 執行任務設定 |
| `--job <name>` | 只執行同名 job，名稱必須完全相符 |
| `--verbose` | 顯示 Git、套件命令與 callback 輸出 |
| `--keep-workspace` | 保留 package job workspace |
| `--allow-callback` | 授權執行可信設定中的 callback |
| `--environment-config <file>` | 套用管理者提供的環境政策 |
| `--inherit-environment` | 完整繼承啟動程序環境，並展開任務欄位中的 `${ENV_VAR}`；僅供可信 repository |

`--environment-config` 與 `--inherit-environment` 不可同時使用。

### 3.5 執行結果與結束碼

```text
PASS download-files       12 files (1.284s)
PASS install-npm          (8.451s)
FAIL download-files       <錯誤內容> (523ms)
```

| 結束碼 | 意義 | 自動化建議 |
| --- | --- | --- |
| `0` | 全部成功 | 繼續後續流程 |
| `1` | 至少一個 job 執行失敗 | 檢查 `FAIL` 與 `--verbose` 輸出 |
| `2` | CLI、設定或啟動流程錯誤 | 修正參數、YAML 或 job 名稱 |
| `130` | 收到 Ctrl+C 或終止訊號 | 視為取消，不發布產物 |

## 4. 任務設定檔完整規格

### 4.1 最上層欄位

```yaml
version: 1
jobs:
  - name: example
    type: urls
    # ...
```

| 欄位 | 型別 | 必填 | 說明 |
| --- | --- | --- | --- |
| `version` | integer | 是 | 目前只接受 `1` |
| `jobs` | list | 是 | 至少一個 job；依列出順序執行 |

所有 `jobs[].name` 都必須非空且不可重複。YAML 欄位名稱區分大小寫。

### 4.2 Job 共用欄位

| 欄位 | 型別 | 必填 | 預設值／說明 |
| --- | --- | --- | --- |
| `name` | string | 是 | job 唯一名稱，供結果與 `--job` 使用 |
| `type` | string | 是 | `urls` 或 `package` |
| `timeout` | duration | 否 | 預設 `10m`，必須大於 0 |
| `callback` | list | 否 | 成功後依序執行的外部程式清單，需 CLI 授權；亦相容舊版單一物件格式 |

Duration 採 Go 格式，可使用 `30s`、`10m`、`1h30m`；不可寫純數字或零值。

## 5. URL 清單下載

### 5.1 設定欄位

| 欄位 | 型別 | 必填 | 預設值／說明 |
| --- | --- | --- | --- |
| `output` | string | 是 | 下載目的目錄 |
| `urlList` | string | 是 | URL 文字清單路徑 |
| `concurrency` | integer | 否 | 預設 `4`，必須至少為 1 |
| `overwrite` | boolean | 否 | 預設 `false`，是否取代既有同名檔案 |

URL 清單每行一個 URL；空白行、前後空白及第一個非空字元為 `#` 的行會被忽略，完全相同的 URL 會去重並保留第一次出現的順序。

```text
# Runtime packages
https://downloads.example.com/app-1.0.0.zip

https://downloads.example.com/checksums.txt
```

只接受 `http` 與 `https`。目的檔名取自 URL path 最後一段，query string 不算檔名。下列 URL 會產生同名衝突並在下載前失敗：

```text
https://a.example/releases/app.zip
https://b.example/mirror/app.zip?token=123
```

### 5.2 場景：首次批次下載

```yaml
version: 1
jobs:
  - name: download-release-files
    type: urls
    urlList: ./downloads.txt
    output: ./artifacts/releases
    concurrency: 4
    timeout: 15m
    overwrite: false
```

```bash
./artifact-downloader validate --config ./artifact.yaml
./artifact-downloader run --config ./artifact.yaml
```

檔案會先寫入目的目錄中的暫存檔，同步完成後再發布成正式檔名；未完成的暫存下載不會當成成功產物。

### 5.3 場景：定期更新既有檔案

將 `overwrite` 設為 `true`：

```yaml
overwrite: true
```

若為 `false` 且目的檔已存在，job 會失敗。工具不比較內容、ETag 或 checksum；若需要驗證雜湊，應以成功後 callback 或外部流程處理。

### 5.4 場景：大量檔案或不穩定網路

- 提高 `concurrency` 可增加吞吐量，但也會提高來源站與本機的連線負載。
- `timeout` 是整個 job 的總期限，不是每個 URL 的期限。
- 本工具目前不提供 retry、續傳、自訂 HTTP header 或 URL 認證欄位；需要時應由受控的 Proxy／下載入口處理，或擴充程式。

## 6. Package 工作

### 6.1 共用欄位

```yaml
- name: package-example
  type: package
  repository:
    url: https://github.com/example/project.git
    ref: main
    depth: 1
  workingDirectory: .
  packageManager: npm
  command:
    action: install
  cache: ./artifacts/npm-cache
  output: ./artifacts/npm-output
  timeout: 30m
```

| 欄位 | 型別 | 必填 | 說明 |
| --- | --- | --- | --- |
| `repository.url` | string | 是 | Git clone URL |
| `repository.ref` | string | 否 | clone 後以 detached HEAD checkout 的 branch、tag 或 commit |
| `repository.depth` | integer | 否 | `0` 表示完整 clone；正數傳給 `--depth`；不可為負數 |
| `repository.gitArgs` | string list | 否 | 插在 Git 子命令前，clone 與 checkout 都套用 |
| `repository.cloneArgs` | string list | 否 | 只插在 `git clone` 後 |
| `workingDirectory` | string | 否 | repository 內執行命令的目錄；空值等同根目錄 |
| `packageManager` | string | 是 | `gradle`、`mvn`、`npm`、`yarn`、`pip` |
| `command.action` | string | 是 | 必須是該 manager 的允許動作 |
| `environment` | string map | 否 | 此 job 的固定環境變數；值可使用受控路徑變數 |
| `cache` | string | 是 | 持久化依賴 cache 目錄 |
| `output` | string | 視情況 | pip 必填；npm 可選；其他 manager 可選 |

`workingDirectory` 不可使用 `..` 或 symlink 逃出 clone 的 repository。

### 6.2 固定命令與產物

| Manager / action | 實際命令 | Cache 設定 | `output` 行為 |
| --- | --- | --- | --- |
| `gradle` / `build` | `gradle build --no-daemon` | `GRADLE_USER_HOME` | 不自動複製 build 產物 |
| `mvn` / `build` | `mvn package --batch-mode -Dmaven.repo.local=<cache>` | 命令參數 | 不自動複製 build 產物 |
| `npm` / `install` | `npm ci --ignore-scripts` | `npm_config_cache` | 有設定時複製至 `<output>/node_modules` |
| `npm` / `install-unlocked` | `npm install --ignore-scripts --no-package-lock` | `npm_config_cache` | 有設定時複製至 `<output>/node_modules` |
| `yarn` / `install` | `yarn install --immutable --ignore-scripts` | `YARN_CACHE_FOLDER` | 不自動複製安裝產物 |
| `pip` / `download` | `python3 -m pip download -r requirements.txt --dest <output>` | `PIP_CACHE_DIR` | 套件檔直接寫入 `output` |

設定檔不能自訂 package executable 或 args。這是安全邊界，也是可預測性的來源。

`environment` 可為單一 package job 加入非敏感固定值：

```yaml
environment:
  CI: "true"
  NODE_OPTIONS: --max-old-space-size=4096
  PACKAGE_CACHE_PATH: ${ARTIFACT_CACHE}
```

值中可使用 `${ARTIFACT_CACHE}`、`${ARTIFACT_OUTPUT}`、`${WORKSPACE}` 與 `${REPOSITORY_DIR}`，並在執行 job 時展開為絕對路徑。環境變數名稱必須符合一般識別字格式。`ARTIFACT_CACHE`、`ARTIFACT_OUTPUT`、`HOME`、`GRADLE_USER_HOME`、`PIP_CACHE_DIR`、`npm_config_cache` 與 `YARN_CACHE_FOLDER` 由工具管理，不能在 job 中覆寫。`environment` 只套用於 package 命令，不會套用到 Git clone 或 callback。

使用 `--inherit-environment` 時，`repository.url`、`repository.ref`、`workingDirectory`、`packageManager`、`command.action`、`cache`、`output`、`urlList`、job `environment`、callback executable 與 args 也可引用啟動程序的 `${ENV_VAR}`：

```bash
export PROJECT=my-project
export REPOSITORY=my-repository
export BRANCH=main
export WORKDIR=.
export PKGMANAGER=npm
export ACTION=install-unlocked
export OUTPUT=./artifacts/npm

./artifact-downloader run \
  --config ./packages.downloader.yaml \
  --inherit-environment
```

```yaml
repository:
  url: https://dev.azure.com/example/${PROJECT}/_git/${REPOSITORY}
  ref: ${BRANCH}
workingDirectory: ${WORKDIR}
packageManager: ${PKGMANAGER}
command:
  action: ${ACTION}
output: ${OUTPUT}
```

缺少任何被引用的主機變數時，工具會在開始 clone／下載前失敗並指出欄位與變數名稱。未加 `--inherit-environment` 時，這些欄位的主機變數引用也會被拒絕；既有 `repository.gitArgs`／`cloneArgs` 的環境展開不受此限制。

Secret 不應直接寫入任務 YAML；請將值留在啟動程序環境，並透過第 8 節環境政策的 `environmentFrom` 映射。

### 6.3 場景：Gradle 專案

```yaml
version: 1
jobs:
  - name: build-gradle
    type: package
    repository:
      url: https://github.com/example/backend.git
      ref: v2.4.0
      depth: 1
    workingDirectory: .
    packageManager: gradle
    command:
      action: build
    cache: ./artifacts/gradle-cache
    timeout: 30m
```

若專案位於 monorepo 的 `backend`，改為 `workingDirectory: backend`。Gradle 的 `build/` 位於暫存 workspace，正常結束後會被清除；此模式主要保留依賴 cache。若必須永久保存 build 產物，repository 的建置邏輯需將檔案寫入環境變數 `ARTIFACT_OUTPUT` 指定的位置，並在 YAML 設定 `output`。

### 6.4 場景：Maven 專案

```yaml
version: 1
jobs:
  - name: build-maven
    type: package
    repository:
      url: https://github.com/example/service.git
      ref: main
      depth: 1
    workingDirectory: service
    packageManager: mvn
    command:
      action: build
    cache: ./artifacts/maven-repository
    timeout: 30m
```

`cache` 是 Maven local repository，而不是最終 JAR 目錄。`target/` 預設位於暫存 workspace 並會被清除。

### 6.5 場景：npm 有 lockfile 的可重現安裝

```yaml
version: 1
jobs:
  - name: install-npm-locked
    type: package
    repository:
      url: https://github.com/example/web.git
      ref: main
      depth: 1
    workingDirectory: frontend
    packageManager: npm
    command:
      action: install
    cache: ./artifacts/npm-cache
    output: ./artifacts/npm-install
    timeout: 20m
```

`install` 使用 `npm ci`，repository 必須有相容的 `package-lock.json` 或 `npm-shrinkwrap.json`。成功後，既有 `<output>/node_modules` 會由本次結果取代。為降低執行不可信程式碼的風險，npm lifecycle scripts 會被停用。

### 6.6 場景：npm 沒有 lockfile

```yaml
packageManager: npm
command:
  action: install-unlocked
cache: ./artifacts/npm-cache
```

此動作不產生 lockfile，依賴解析結果可能隨 registry 內容改變。可行時應先在原專案建立並審查 lockfile，再改用 `install`。

### 6.7 場景：Yarn immutable 安裝

```yaml
version: 1
jobs:
  - name: install-yarn
    type: package
    repository:
      url: https://github.com/example/portal.git
      ref: main
      depth: 1
    workingDirectory: .
    packageManager: yarn
    command:
      action: install
    cache: ./artifacts/yarn-cache
    timeout: 20m
```

Yarn 使用 `--immutable --ignore-scripts`。lockfile 若需修改，命令會失敗。工具不會把 Yarn 安裝結果自動複製到 `output`。

### 6.8 場景：pip 下載離線套件

```yaml
version: 1
jobs:
  - name: download-python-wheels
    type: package
    repository:
      url: https://github.com/example/python-service.git
      ref: main
      depth: 1
    workingDirectory: .
    packageManager: pip
    command:
      action: download
    cache: ./artifacts/pip-cache
    output: ./artifacts/pip-packages
    timeout: 30m
```

`requirements.txt` 必須位於 `workingDirectory`。pip 只下載、不安裝，並將 wheel 或 source distribution 寫入 `output`。

### 6.9 場景：一份設定執行多種工作

```yaml
version: 1
jobs:
  - name: download-binaries
    type: urls
    urlList: ./downloads.txt
    output: ./artifacts/binaries
    concurrency: 4
    timeout: 10m
    overwrite: false

  - name: download-python-dependencies
    type: package
    repository:
      url: https://github.com/example/service.git
      ref: main
      depth: 1
    workingDirectory: .
    packageManager: pip
    command:
      action: download
    cache: ./artifacts/pip-cache
    output: ./artifacts/pip-packages
    timeout: 30m
```

開發時可用 `--job download-binaries` 單獨重跑；排程或 CI 則不帶 `--job` 執行全部。

## 7. Git 存取場景

### 7.1 Shallow clone 與 clone 最佳化

```yaml
repository:
  url: https://github.com/example/large-repo.git
  ref: main
  depth: 1
  cloneArgs:
    - --no-tags
    - --single-branch
    - --filter=blob:none
```

實際呼叫順序為：

```text
git <gitArgs> clone <cloneArgs> --depth <depth> -- <url> <destination>
git <gitArgs> checkout --detach <ref>
```

`gitArgs` 與 `cloneArgs` 中的 `${ENV_VAR}` 會從 Artifact Downloader 啟動程序環境展開；只要引用的變數不存在，job 就會失敗。這項展開與 package 子程序的環境政策互相獨立。

### 7.2 Azure DevOps PAT

```yaml
repository:
  url: https://dev.azure.com/my-org/my-project/_git/my-repository
  ref: main
  depth: 1
  gitArgs:
    - -c
    - "http.extraHeader=AUTHORIZATION: basic ${ADO_AUTH_HEADER}"
  cloneArgs:
    - --no-tags
```

```bash
export ADO_PAT='your-personal-access-token'
export ADO_AUTH_HEADER="$(printf ':%s' "$ADO_PAT" | base64)"
unset ADO_PAT
./artifact-downloader run --config ./artifact.yaml --verbose
unset ADO_AUTH_HEADER
```

PAT 至少需要 repository 讀取權限。不要把 PAT 寫入 YAML 或 repository URL。

### 7.3 Git HTTP Proxy

```yaml
repository:
  url: https://git.example.com/team/project.git
  gitArgs:
    - -c
    - "http.proxy=${GIT_HTTP_PROXY}"
```

```bash
export GIT_HTTP_PROXY='http://proxy.example.com:8080'
./artifact-downloader run --config ./artifact.yaml
```

一般 HTTP 下載與 package manager 的 Proxy，則應透過啟動程序的 `HTTP_PROXY`、`HTTPS_PROXY`、`NO_PROXY` 或環境政策傳遞。

### 7.4 SSH repository

```yaml
repository:
  url: git@github.com:example/private-project.git
  ref: main
  depth: 1
```

工具沒有管理 SSH key；應由執行帳號預先設定 key、`known_hosts` 與 ssh-agent。Git clone 發生在 package 命令環境政策套用之前，因此使用啟動程序可用的 Git／SSH 環境。

## 8. 環境政策設定檔

Package 命令預設使用內建最小環境，只繼承 `PATH`、locale、暫存目錄及常見大小寫 Proxy 變數。任務 YAML 可提供個別 job 的非敏感固定值；使用者明確加上 `--inherit-environment` 時，也可引用主機變數並完整繼承環境。管理者可另建受信任政策檔：

```yaml
version: 1

minimal:
  inherit:
    - PATH
    - LANG
    - LC_ALL
    - TMPDIR
    - HTTP_PROXY
    - HTTPS_PROXY
    - NO_PROXY
  values:
    CI: "true"

packageManagers:
  pip:
    inherit:
      - VIRTUAL_ENV
      - SSL_CERT_FILE
      - REQUESTS_CA_BUNDLE
    values:
      PIP_DISABLE_PIP_VERSION_CHECK: "1"
    environmentFrom:
      PIP_INDEX_URL:
        source: COMPANY_PIP_INDEX_URL
        required: true

  npm:
    inherit:
      - NODE_EXTRA_CA_CERTS
    environmentFrom:
      npm_config_registry:
        source: COMPANY_NPM_REGISTRY
        required: false
```

執行方式：

```bash
export COMPANY_PIP_INDEX_URL='https://user:token@packages.example.com/simple'
./artifact-downloader run \
  --config ./artifact.yaml \
  --environment-config /secure/path/environment.yaml
```

### 8.1 政策欄位表

| 欄位 | 說明 |
| --- | --- |
| `version` | 目前只接受 `1` |
| `minimal.inherit` | 所有 package manager 可從啟動程序繼承的變數名稱 |
| `minimal.values` | 所有 package manager 使用的固定值 |
| `packageManagers.<manager>.inherit` | 特定 manager 額外繼承的名稱 |
| `packageManagers.<manager>.values` | 特定 manager 的固定值 |
| `packageManagers.<manager>.environmentFrom.<target>.source` | 把啟動程序的 `source` 值映射成子程序的 `target` |
| `packageManagers.<manager>.environmentFrom.<target>.required` | `true` 表示來源未設定時 job 失敗；預設 `false` |

Manager key 必須使用小寫標準名稱：`gradle`、`mvn`、`npm`、`yarn`、`pip`。環境變數名稱必須符合一般識別字格式。

不可由政策設定或繼承的保留變數：

```text
ARTIFACT_CACHE
ARTIFACT_OUTPUT
GRADLE_USER_HOME
PIP_CACHE_DIR
npm_config_cache
YARN_CACHE_FOLDER
HOME
```

`values` 應只放非敏感固定值。Secret 請保留在執行環境，並用 `environmentFrom` 映射；政策檔應由管理者控管，不能放在待 clone 的 repository 中。

### 8.2 場景：公司私有套件來源與 CA

1. 在安全的 CI secret store 設定 `COMPANY_PIP_INDEX_URL`、憑證路徑等來源變數。
2. 以 `environmentFrom` 將 secret 映射至 `PIP_INDEX_URL`。
3. 以 `inherit` 明確允許 `SSL_CERT_FILE` 或 `NODE_EXTRA_CA_CERTS`。
4. 使用 `required: true` 避免缺少認證時意外連到公開 registry。

### 8.3 場景：完整繼承環境進行除錯

```bash
./artifact-downloader run \
  --config ./artifact.yaml \
  --inherit-environment \
  --verbose
```

這會把 token、雲端憑證或代理設定暴露給 repository 中可能執行的建置邏輯，只可用於可信 repository。正式環境應優先撰寫最小權限政策。

## 9. Callback

每個 job 可設定多個成功後 callback：

```yaml
callback:
  - executable: ./scripts/verify-checksum.sh
    args:
      - ${ARTIFACT_OUTPUT}
  - executable: ./scripts/download-complete.sh
    args:
      - ${ARTIFACT_OUTPUT}
      - --cache
      - ${ARTIFACT_CACHE}
      - --notify
```

```bash
./artifact-downloader run \
  --config ./artifact.yaml \
  --allow-callback
```

規則如下：

- 預設停用；設定中只要選定執行的 job 含 callback，而未帶 `--allow-callback`，流程會在執行任何 job 前拒絕啟動。
- 只在該 job 主流程成功後執行，並依清單中的設定順序逐一等待完成。
- 任一 callback 非零結束會停止後續 callback，並使 job 失敗。
- 工作目錄是任務 YAML 所在目錄。
- 不透過 shell，`args` 每一項都是獨立參數；pipe、redirect、`$()` 不會由 shell 解讀。
- `${ARTIFACT_OUTPUT}`、`${ARTIFACT_CACHE}` 可用於 executable 與 args；未設定的路徑展開成空字串。
- Callback 完整繼承 Artifact Downloader 程序環境，因此只能授權可信設定。
- Callback 與主流程共用同一個 job timeout，應預留足夠時間。

舊版的單一物件格式仍可使用，載入時會視為只含一項的 callback 清單。

適用場景包括 checksum 驗證、產物壓縮、寫入完成旗標或呼叫既有通知程式。若需要複合 shell 語法，應將邏輯寫在經審查的腳本內，再把該腳本設為 `executable`。

## 10. CI／排程建議流程

```bash
set -e
go build -o artifact-downloader ./cmd/artifact-downloader
./artifact-downloader validate --config ./artifact.yaml
./artifact-downloader run \
  --config ./artifact.yaml \
  --environment-config /secure/path/environment.yaml \
  --verbose
```

建議：

- Cache 與 output 放在持久磁碟；不要放在 package 暫存 workspace。
- 將任務設定納入版本控制，環境政策由管理者另行控管。
- Secret 使用 CI secret store 注入，切勿提交到 YAML。
- 保留執行摘要及 verbose log，但確認外部工具不會把憑證印出。
- 不可信 repository 應在 container 或 OS sandbox 執行。Gradle、Maven 等建置本身仍可執行 repository 中的程式碼。
- 同一個 output/cache 若可能由多個程序同時寫入，應由排程層避免並行；工具未提供跨程序鎖定。

## 11. 常見錯誤與處理

| 現象／訊息 | 常見原因 | 處理方式 |
| --- | --- | --- |
| `unknown command` | 子命令拼錯 | 使用 `help` 檢查語法 |
| `invalid configuration: ... field ... not found` | YAML 欄位拼錯或不支援 | 對照本手冊欄位，注意大小寫 |
| `duplicate job name` | job 名稱重複 | 為每個 job 設定唯一名稱 |
| `job ... not found` | `--job` 名稱不符 | 使用 YAML 中完整名稱 |
| `destination already exists` | `overwrite: false` 且檔案已存在 | 確認後改為 `true` 或換 output |
| `same filename` | 不同 URL 的 path 最後一段相同 | 分拆 job/output，或調整來源 URL 檔名 |
| `URL does not contain a safe filename` | URL 結尾沒有有效檔名 | 使用直接指向檔案的 URL |
| `unexpected HTTP status` | 404、403、伺服器錯誤 | 檢查 URL、權限與 Proxy；URL 模式不支援自訂 header |
| `job timed out` | 網路或 clone／build 超過總期限 | 增加 `timeout`，用 `--verbose` 找慢點 |
| `callback is disabled` | YAML 有 callback 但未授權 | 審查設定後才加 `--allow-callback` |
| `environment variable ... is not set` | `gitArgs`／`cloneArgs` 引用未設定變數 | 執行前 export 對應變數 |
| `required environment source ... is not set` | 政策的必要 secret 未注入 | 在安全環境設定 `source` 變數 |
| `execute "git/npm/..."` | 外部工具不在 PATH 或命令失敗 | 安裝工具並使用 `--verbose` 查看原因 |
| npm `ci` 失敗 | lockfile 缺少或與 package.json 不一致 | 修正 lockfile；無 lockfile 才使用 `install-unlocked` |
| Yarn immutable 失敗 | 安裝需要修改 lockfile | 在原專案更新並提交 lockfile |
| pip 找不到 requirements | 檔案不在執行目錄 | 修正 `workingDirectory` |
| `workingDirectory resolves outside...` | `..` 或 symlink 離開 repository | 使用 repository 內的正常子目錄 |

排錯順序：先 `validate`，再以 `--job` 縮小範圍，接著開啟 `--verbose`；package job 必要時加 `--keep-workspace` 檢查 clone 結果與工作目錄。

## 12. 設定撰寫檢查表

- `version` 是 `1`，且至少有一個 job。
- job `name` 唯一，`type` 只使用 `urls` 或 `package`。
- 所有相對路徑均以 YAML 位置為基準重新確認。
- URL 清單沒有不同 URL 的同名檔案，並依需求選擇 `overwrite`。
- Package manager 與 action 組合存在於固定命令表。
- `workingDirectory` 指向 repository 內含必要專案檔案的目錄。
- pip 有 `output`；npm 若要保留 `node_modules` 也有 `output`。
- `timeout` 涵蓋下載、clone、命令與 callback 的總耗時。
- 私有 Git 認證與 package secret 不直接寫入 YAML。
- 正式環境採最小權限環境政策；只對可信來源啟用 callback 或完整環境繼承。
- 執行前先跑 `validate`，自動化流程依結束碼決定是否發布產物。
