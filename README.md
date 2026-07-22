## Artifact Downloader

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

`--verbose` 會顯示 Git 與外部命令輸出；`--keep-workspace` 會保留 package job 的暫存目錄供除錯。

### URL 模式

```yaml
version: 1

jobs:
  - name: download-files
    type: urls
    output: ./artifacts/files
    urlList: ./downloads.txt
    concurrency: 4
    timeout: 10m
    overwrite: false
```

`downloads.txt` 每行放置一個 URL。空白行與以 `#` 開頭的註解會被忽略。URL 必須包含安全的檔名；不同 URL 不可產生相同檔名。

設定檔內的 `output` 與 `urlList` 均相對於 YAML 所在目錄解析。

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
    workingDirectory: backend
    packageManager: gradle
    command:
      executable: ./gradlew
      args:
        - build
        - --no-daemon
    cache: ./artifacts/gradle-cache
    timeout: 30m
```

工具會依序：

1. 建立隔離的暫存 workspace。
2. 使用系統的 `git` clone repository，並 checkout `ref`。
3. 切換至 `workingDirectory`。
4. 將套件管理器的 cache 指向設定的 `cache` 目錄。
5. 執行 `command.executable` 與 `command.args`，由建構或安裝流程下載依賴。
6. 將 cache 保留在 workspace 外；若命令自行寫入 `output`，該產物也會保留。
7. 成功或失敗後清除 workspace，除非指定 `--keep-workspace`。

`cache` 是 package job 的必填欄位。`output` 是選填路徑，工具不會自動複製建構產物；repository 內的建構命令或 wrapper script 必須明確將產物寫入 `${ARTIFACT_OUTPUT}`。

工具會依 `packageManager` 自動設定 cache：

| Package manager | Cache 設定 |
| --- | --- |
| Gradle | `GRADLE_USER_HOME=<cache>` |
| Maven | 命令附加 `-Dmaven.repo.local=<cache>` |
| npm | `npm_config_cache=<cache>` |
| Yarn | `YARN_CACHE_FOLDER=<cache>` |
| pip | `PIP_CACHE_DIR=<cache>` |

常見建構或安裝命令如下；實際命令仍應依 repository 的 wrapper、lockfile 與 CI 規則調整：

| Package manager | 命令範例 |
| --- | --- |
| Gradle | `./gradlew build --no-daemon` |
| Maven | `./mvnw package` |
| npm | `npm ci` |
| Yarn | `yarn install --immutable` |
| pip | `python -m pip install -r requirements.txt` |

執行命令時會提供以下環境變數，也會在 `command.executable`、`command.args` 與自訂 `environment` 的值中展開相同格式的受控變數：

- `${ARTIFACT_CACHE}`：`cache` 的絕對路徑。
- `${ARTIFACT_OUTPUT}`：`output` 的絕對路徑；未設定時為空字串。
- `${WORKSPACE}`：暫存 workspace。
- `${REPOSITORY_DIR}`：clone 後的 repository 根目錄。

例如 pip 只下載套件而不安裝：

```yaml
output: ./artifacts/pip-files
command:
  executable: python3
  args:
    - -m
    - pip
    - download
    - --destination-directory
    - ${ARTIFACT_OUTPUT}
```

命令不會交給 shell 執行；只有上述四個變數會由工具展開，避免 shell quoting 與 command injection 問題。

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
