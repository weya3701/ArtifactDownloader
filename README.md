## Artifact Downloader

完整的安裝步驟、CLI 操作、設定欄位、各套件管理器與私有來源、Proxy、Callback、CI 及排錯場景，請參閱[使用者操作與設定手冊](docs/USER_GUIDE.zh-TW.md)。

Artifact Downloader 是以 YAML 操作的命令列工具，支援兩種工作模式：

- 從文字檔讀取多個 HTTP/HTTPS URL，並行下載至指定目錄。
- Clone 開發專案、切換至定義檔目錄，再執行建構或安裝命令；套件工具會在過程中自動下載依賴。

定義檔模式可用於 Gradle、npm、Maven、pip 與 Yarn。設定檔提供實際命令，讓各專案使用原生建構流程解析依賴。

### 建置

需要 Go 1.25 或更新版本：

```bash
go build -o artifact-downloader ./cmd/artifact-downloader
```

### CLI

驗證設定但不執行：

```bash
./artifact-downloader validate --config examples/urls.yaml
```

執行全部工作：

```bash
./artifact-downloader run --config artifact.yaml
```

只執行指定工作：

```bash
./artifact-downloader run --config artifact.yaml --job download-files
```

`--verbose` 會顯示 Git 與外部命令輸出；`--keep-workspace` 會保留 package job 的暫存目錄供除錯。Callback 預設停用，只有信任設定檔時才應使用 `--allow-callback`。

Package job 預設只取得內建允許的最小環境。由管理者提供受信任的環境政策：

```bash
./artifact-downloader run \
  --config artifact.yaml \
  --environment-config examples/environment.yaml
```

除錯時也可完整繼承 Artifact Downloader 程序的原始環境，並在執行前將任務欄位中的 `${ENV_VAR}` 展開：

```bash
./artifact-downloader run \
  --config artifact.yaml \
  --inherit-environment
```

`--environment-config` 與 `--inherit-environment` 互斥。完整繼承可能把 token 或雲端憑證暴露給 repository 的建構邏輯，只能用於可信 repository。任務欄位引用的變數不存在時，job 會在 clone 或下載開始前失敗並指出欄位名稱。

### URL 模式

```yaml
version: 1

jobs:
  - name: download-files
    type: urls
    output: ./artifacts/files
    urlList: ./downloads.txt
    concurrency: 4
    requestDelay:
      min: 1s
      max: 3s
    headers:
      User-Agent: ArtifactDownloader/1.0
      Accept: "*/*"
    timeout: 10m
    overwrite: false
```

`downloads.txt` 每行放置一個 URL。空白行與以 `#` 開頭的註解會被忽略。URL 必須包含安全的檔名；不同 URL 不可產生相同檔名。

設定檔內的 `output` 與 `urlList` 均相對於 YAML 所在目錄解析。`requestDelay` 是選填設定；啟用時，
工具會在全域相鄰 request 的開始時間之間隨機等待 `min` 至 `max`，同時仍以 `concurrency`
限制最多並行 request 數。`min` 與 `max` 必須同時提供、皆為正值，且 `min` 不可大於 `max`。
`headers` 可設定每個 GET request 使用的 header；header 值若引用 `${ENV_VAR}`，執行時需加
`--inherit-environment`，且 secret 不應直接寫入 YAML。

### 下載完成 Callback

每個 job 都可設定多個 `callback`。Callback 可執行任意外部程式，因此預設停用；只有設定檔受信任時才可用 `run --allow-callback` 開啟。主要下載流程成功後，callback 會依設定順序逐一執行；任一命令失敗時會停止後續 callback，該 job 也會回報失敗。命令與參數需分開設定：

```yaml
callback:
  - executable: ./scripts/verify-checksum.sh
    args:
      - ${ARTIFACT_OUTPUT}
  - executable: ./scripts/download-complete.sh
    args:
      - ${ARTIFACT_OUTPUT}
      - --notify
```

Callback 的工作目錄是 YAML 設定檔所在目錄。命令不會交給 shell 執行，因此 `args` 中的每一項都會原樣作為獨立參數傳入。可在 `executable` 與 `args` 使用 `${ARTIFACT_OUTPUT}` 和 `${ARTIFACT_CACHE}`；未設定的路徑會展開為空字串。原本的單一 callback 物件格式仍可使用。

### Package 模式

```yaml
version: 1

jobs:
  - name: download-java-dependencies
    type: package
    repository:
      url: https://github.com/example/project.git
      ref: main
      depth: 1
    # 選填；在 ADO 可用 .，直接使用設定檔所在的 pipeline workspace。
    workspace: .
    workingDirectory: backend
    packageManager: gradle
    command:
      action: build
    environment:
      CI: "true"
      BUILD_CACHE: ${ARTIFACT_CACHE}
    cache: ./artifacts/gradle-cache
    timeout: 30m
```

工具會依序：

1. 未設定 `workspace` 時建立隔離的暫存 workspace；有設定時使用指定目錄。
2. 使用系統的 `git` clone repository，並 checkout `ref`。
3. 切換至 `workingDirectory`。
4. 將套件管理器的 cache 指向設定的 `cache` 目錄。
5. 依 `packageManager` 與 `command.action` 選擇內建的固定命令，由建構或安裝流程下載依賴。
6. 將 cache 保留在設定的 `cache` 路徑；npm job 若設定 `output`，會將安裝完成的
   `node_modules` 保留到 `<output>/node_modules`。其他 manager 若命令自行寫入
   `output`，該產物也會保留。
7. 自動建立的暫存 workspace 會在成功或失敗後清除，除非指定 `--keep-workspace`；明確設定的 `workspace` 一律保留並交由呼叫端管理。

`workspace`、`cache` 與 `output` 的相對路徑都以 YAML 所在目錄為基準。ADO pipeline 若已提供一次性 workspace，可設定 `workspace: .`；repository 會 clone 到 `./repository`，建構內容不會由工具清除。該目錄已存在 repository 時，Git 會拒絕再次 clone，因此重跑時應使用乾淨的 pipeline workspace 或不同路徑。

`cache` 是 package job 的必填欄位。除 pip `download` action 外，`output` 是選填路徑。npm job 設定 `output` 時，工具會在安裝成功後複製 `node_modules`；未設定時仍只執行安裝。其他 package manager 不會自動複製建構產物，repository 內的建構邏輯必須明確將產物寫入 `ARTIFACT_OUTPUT` 指定的目錄。

Package job 不接受自訂 executable 或 args；無法匹配的 manager/action 會在設定驗證階段被拒絕。可透過 `environment` map 為個別 package job 設定固定環境變數，值中可使用 `${ARTIFACT_CACHE}`、`${ARTIFACT_OUTPUT}`、`${WORKSPACE}` 與 `${REPOSITORY_DIR}`。搭配 `--inherit-environment` 時，也可在 `repository.url`、`repository.ref`、`workspace`、`workingDirectory`、`packageManager`、`command.action`、`cache`、`output`、`urlList`、URL `headers` 值、job `environment` 與 callback 設定中引用主機的 `${ENV_VAR}`。工具只使用系統安裝的 package manager，不執行 repository 內的 `gradlew` 或 `mvnw` wrapper。

目前支援的動作與固定命令如下：

| Package manager | Action | 固定命令 |
| --- | --- | --- |
| Gradle | `build` | `gradle build --no-daemon` |
| Maven | `build` | `mvn package --batch-mode` |
| npm | `install` | `npm ci --ignore-scripts` |
| npm | `install-unlocked` | `npm install --ignore-scripts --no-package-lock` |
| Yarn | `install` | `yarn install --immutable --ignore-scripts` |
| pip | `download` | `python3 -m pip download -r requirements.txt --dest <output>` |

npm 專案有 `package-lock.json` 或 `npm-shrinkwrap.json` 時應優先使用
`install`，以取得可重現的依賴版本。只有無 lockfile 的專案才使用
`install-unlocked`；它不會產生 lockfile，實際解析的依賴版本可能隨 registry
內容變動：

```yaml
packageManager: npm
command:
  action: install-unlocked
```

工具會依 `packageManager` 自動設定 cache：

| Package manager | Cache 設定 |
| --- | --- |
| Gradle | `GRADLE_USER_HOME=<cache>` |
| Maven | 命令附加 `-Dmaven.repo.local=<cache>` |
| npm | `npm_config_cache=<cache>` |
| Yarn | `YARN_CACHE_FOLDER=<cache>` |
| pip | `PIP_CACHE_DIR=<cache>` |

Package 命令使用最小環境，不會繼承程序中的任意 token 或 secret。保留的系統變數僅包含 `PATH`、locale、暫存目錄及常見 HTTP proxy 變數，並提供：

- `${ARTIFACT_CACHE}`：`cache` 的絕對路徑。
- `${ARTIFACT_OUTPUT}`：`output` 的絕對路徑；未設定時為空字串。

`environment` 適合 `CI: "true"`、`NODE_OPTIONS` 等非敏感固定值。Secret 不應直接寫進任務 YAML，請使用下節的 `environmentFrom`。`ARTIFACT_CACHE`、`ARTIFACT_OUTPUT`、`HOME` 與套件管理器 cache 變數由工具管理，不能在 `environment` 覆寫。

Shell 提供任務參數的範例：

```bash
export PROJECT=my-project
export REPOSITORY=my-repository
export BRANCH=main
export WORKDIR=.
export PKGMANAGER=npm
export ACTION=install-unlocked
export OUTPUT=./artifacts/npm

./artifact-downloader run --config packages.downloader.yaml --inherit-environment
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

例如 pip 只下載套件而不安裝：

```yaml
output: ./artifacts/pip-files
command:
  action: download
```

命令不會交給 shell 執行。固定 executable、固定參數及最小環境可避免設定檔直接注入任意命令；但 Gradle、Maven 與其他套件工具仍可能執行 repository 內的建構邏輯，因此不可信 repository 仍應在容器或 OS sandbox 中執行。

### 環境政策

環境政策負責所有 job 的共同環境、特定 package manager 設定及 secret 映射；任務 YAML 的 `environment` 可提供個別 job 的固定值，或在 `--inherit-environment` 模式引用主機變數，但不能選擇 profile。只有啟動 Artifact Downloader 的管理者能透過 CLI 選擇政策。

範本位於 `examples/environment.yaml`，主要欄位：

```yaml
version: 1

minimal:
  inherit:
    - PATH
    - LANG
  values: {}

packageManagers:
  pip:
    inherit:
      - VIRTUAL_ENV
      - SSL_CERT_FILE
    values:
      PIP_DISABLE_PIP_VERSION_CHECK: "1"
    environmentFrom:
      PIP_INDEX_URL:
        source: COMPANY_PIP_INDEX_URL
        required: false
```

- `minimal.inherit`：所有 package job 可從啟動程序繼承的變數。
- `minimal.values`：所有 package job 使用的固定非敏感值。
- `packageManagers.<manager>.inherit`：指定 package manager 額外繼承的變數。
- `values`：該 package manager 使用的固定非敏感值。
- `environmentFrom`：將啟動程序中的受保護變數映射成子程序需要的名稱；`required: true` 時缺少來源會使 job 失敗。

Secret 值不可直接寫入政策檔。`ARTIFACT_CACHE`、`ARTIFACT_OUTPUT`、`HOME` 及各套件管理器 cache 變數由工具管理，政策若嘗試設定或繼承這些保留名稱，驗證會失敗。政策檔應存放在只有管理者可修改的位置，不應放在待下載的 repository 中。

### Git clone 參數、ADO PAT 與 Proxy

Repository 支援兩種額外參數：

- `gitArgs`：放在 Git 子命令之前，clone 與 checkout 都會套用。適合 `git -c ...` 設定。
- `cloneArgs`：只放在 `git clone` 後。適合 `--no-tags`、`--single-branch` 或 `--filter=blob:none`。

參數中的 `${ENV_VAR}` 會從 Artifact Downloader 程序的環境變數展開；變數未設定時 job 會失敗。PAT 不應直接寫入 YAML，也不建議放在 repository URL。

Azure DevOps HTTPS repository 範例：

```yaml
repository:
  url: https://dev.azure.com/my-organization/my-project/_git/my-repository
  ref: main
  depth: 1
  gitArgs:
    - -c
    - "http.extraHeader=AUTHORIZATION: basic ${ADO_AUTH_HEADER}"
  cloneArgs:
    - --no-tags
```

執行前，將 `:<PAT>` 轉成 HTTP Basic authentication 所需的 Base64 值：

```bash
export ADO_PAT='your-personal-access-token'
export ADO_AUTH_HEADER="$(printf ':%s' "$ADO_PAT" | base64)"
unset ADO_PAT

./artifact-downloader run --config artifact.yaml --verbose
```

PAT 至少需要目標 repository 的讀取權限。使用 `http.extraHeader` 可避免 PAT 出現在 repository URL、Git remote 設定及一般錯誤訊息中。

HTTP Proxy 範例：

```yaml
repository:
  url: https://dev.azure.com/my-organization/my-project/_git/my-repository
  gitArgs:
    - -c
    - "http.proxy=${GIT_HTTP_PROXY}"
```

```bash
export GIT_HTTP_PROXY='http://proxy.example.com:8080'
./artifact-downloader run --config artifact.yaml
```

完整 Git 呼叫順序如下：

```text
git <gitArgs> clone <cloneArgs> --depth <depth> -- <url> <destination>
git <gitArgs> checkout --detach <ref>
```

### 結束代碼

- `0`：所有工作成功。
- `1`：至少一個工作執行失敗。
- `2`：CLI 或設定錯誤。
- `130`：收到中斷訊號。

* * *
