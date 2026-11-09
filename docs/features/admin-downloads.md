# Downloads MP3 (`/admin/downloads`)

## Para que serve

Ferramenta interna de **admin** para converter uma lista de vídeos do
YouTube em arquivos `.mp3` e baixar tudo num único `.zip`. Não tem
relação com a observabilidade do Ndovu: é um utilitário operacional,
útil para tirar o áudio de conteúdo próprio (podcasts, aulas, demos)
sem depender de site de terceiros.

> ⚠️ **Uso permitido:** apenas conteúdo próprio ou com licença que
> autorize o download. Baixar vídeos de terceiros viola os Termos de
> Serviço do YouTube. A ferramenta é restrita a admin justamente para
> manter o uso controlado — não existe rota pública.

## Onde fica

- Rota: `/admin/downloads`
- Arquivos:
  - `dashboard/src/app/admin/downloads/page.tsx` (UI)
  - `dashboard/src/app/api/admin/downloads/**` (BFF)
  - `backend/internal/adapter/httpapi/download_handlers.go` (API)
  - `backend/internal/downloader/` (jobs, yt-dlp, zip)
  - `backend/cmd/downloader/` (serviço HTTP interno)
- Endpoints (todos admin):
  - `POST /v1/admin/downloads` — cria o job
  - `GET /v1/admin/downloads/{id}` — progresso
  - `GET /v1/admin/downloads/{id}/archive` — baixa o `.zip`
- Serviço: `downloader` no `docker-compose.yml` (porta interna 8090)
- Papel mínimo: **admin**

## Como usar

1. Abra **Downloads MP3** no menu de administração.
2. Cole um link por linha (máximo **10 vídeos por job**). Formatos
   aceitos: `youtube.com/watch?v=…`, `youtu.be/…`, `/shorts/…`,
   `/live/…`, `m.youtube.com` e `music.youtube.com`.
3. Clique **Criar job**. A tela acompanha o progresso a cada 2s
   (`Na fila` → `Processando` → `Pronto`).
4. Quando ficar **Pronto**, clique **Baixar .zip**. O arquivo vem com
   os `.mp3` na raiz e, se algum vídeo falhou, um `falhas.txt` com a
   URL e o motivo.
5. O zip e os áudios ficam no disco do serviço por **24h** (TTL
   configurável); depois são apagados.

## Como funciona

```
Browser → BFF Next (/api/admin/downloads) → API Go (/v1/admin/downloads)
                                              │ token interno
                                              ▼
                                   ndovu-downloader (python + yt-dlp + ffmpeg)
                                              │
                                   /data/downloads/<job>/{audio,bundle.zip}
```

- A API **não** executa `yt-dlp`: só valida os links (host YouTube),
  repassa o job e faz streaming do zip. A imagem da API é distroless,
  sem python/ffmpeg.
- O downloader processa **um vídeo por vez** dentro do job e
  `NDOVU_DOWNLOADER_CONCURRENCY` jobs em paralelo (default 1).
- Cada vídeo tem timeout próprio (`NDOVU_DOWNLOADER_VIDEO_TIMEOUT_SECONDS`,
  default 600s). Falha em um vídeo não derruba o job: os demais seguem
  e o erro entra em `falhas.txt`.
- A conversão usa `--audio-format mp3 --audio-quality 0` (VBR ~245
  kbps). É reencode *lossy* do AAC/Opus do YouTube — não existe
  "MP3 original".
- O estado do job é persistido em `job.json`; se o serviço reiniciar
  no meio, o job vira `error` ("interrompido por restart") em vez de
  ficar preso em `running`.

## Configuração

| Env | Default | Onde |
|---|---|---|
| `NDOVU_DOWNLOADER_TOKEN` | — (obrigatório no serviço) | API + downloader |
| `NDOVU_DOWNLOADER_URL` | vazio = feature desligada (503) | API |
| `NDOVU_DOWNLOADER_MAX_VIDEOS` | `10` | API + downloader |
| `NDOVU_DOWNLOADER_CONCURRENCY` | `1` | downloader |
| `NDOVU_DOWNLOADER_VIDEO_TIMEOUT_SECONDS` | `600` | downloader |
| `NDOVU_DOWNLOADER_JOB_TTL_HOURS` | `24` | downloader |
| `NDOVU_DOWNLOADER_DATA_DIR` | `/data/downloads` | downloader |
| `NDOVU_YTDLP_BIN` | `yt-dlp` | downloader |
| `NDOVU_FFMPEG_LOCATION` | vazio (PATH) | downloader |
| `NDOVU_YTDLP_COOKIES` | vazio (sem cookies) | downloader |

Sem `NDOVU_DOWNLOADER_URL` a tela responde 503 ("downloader
desabilitado") — o resto do Ndovu continua funcionando normalmente.

## YouTube bloqueando o IP ("Sign in to confirm you're not a bot")

IP de datacenter (VPS, cloud) é barrado pelo YouTube mesmo para conteúdo
próprio. O yt-dlp responde com `Sign in to confirm you're not a bot`, e
trocar o *player client* (`android`, `ios`, `tv_embedded`, `web_embedded`,
`mweb`, `android_vr`) **não resolve** — testado. A saída é fornecer cookies
de uma sessão logada:

1. Exporte `cookies.txt` (formato Netscape) de um navegador logado no
   YouTube — extensão "Get cookies.txt LOCALLY" ou `yt-dlp
   --cookies-from-browser`.
2. Coloque o arquivo em `secrets/cookies.txt` (local) ou
   `/srv/apps/ndovu/secrets/cookies.txt` (produção). O diretório é montado
   read-only em `/secrets` e está no `.gitignore`.
3. Em produção, o container roda como uid `10001`:
   `chown 10001:10001 secrets/cookies.txt && chmod 600 secrets/cookies.txt`.
4. Defina `NDOVU_YTDLP_COOKIES=/secrets/cookies.txt` e recrie o serviço:
   `docker compose ... up -d downloader`.

> ⚠️ Cookies são credenciais da conta: expiram (às vezes em horas) e o uso
> para download viola os Termos do YouTube — há risco de a conta ser
> suspensa. Prefira uma conta descartável e nunca commite o arquivo.

## Papéis (RBAC)

- **admin**: cria jobs, vê progresso e baixa o zip.
- **editor / viewer**: não veem o item no menu e recebem 403 se
  chamarem a API direto (o grupo `/v1/admin` exige `role=admin`).
- Cada criação de job entra no [audit log](admin-audit-log.md) como
  `download.create`.

## Como demonstrar

> "Preciso do áudio de cinco aulas que eu mesmo publiquei. Colo os
> cinco links, crio o job, e em alguns minutos baixo um zip com os
> MP3. Se um vídeo falhar, o `falhas.txt` me diz qual e por quê."

## Perguntas frequentes

- **Dá para usar a API oficial do YouTube?** Não para baixar: a
  YouTube Data API v3 só devolve metadados. O download usa `yt-dlp`,
  que extrai o stream do player.
- **Por que às vezes falha com "Sign in to confirm you're not a bot"
  ou "The page needs to be reloaded"?** O YouTube bloqueia IP de
  datacenter e muda o player com frequência. O `yt-dlp` precisa ser
  atualizado (o rebuild da imagem faz `pip install -U yt-dlp`) e, em
  casos extremos, exige cookies de uma conta (ver seção acima).
- **O zip pode ser baixado mais de uma vez?** Sim, enquanto o job
  não expirar (24h). O arquivo é servido do disco do downloader.
- **Playlist?** Não. Cada link é um vídeo; `--no-playlist` garante
  que um link de watch com `list=` baixe só o vídeo.
- **O que acontece se todos os vídeos falharem?** O job termina em
  `error` e não gera zip.

## Referências

- `backend/internal/downloader/downloader.go`
- `backend/cmd/downloader/main.go`
- `backend/internal/adapter/httpapi/download_handlers.go`
- `backend/internal/adapter/downloaderclient/client.go`
- `dashboard/src/app/admin/downloads/page.tsx`
- `docs/features/admin-keys.md` (padrão de tela admin)
