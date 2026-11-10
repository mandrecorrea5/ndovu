# Distribuição dos SDKs do Ndovu — discovery e recomendação

**Pergunta:** onde publicar os SDKs do Ndovu? Vale subir um Nexus numa VPS?
Existe opção grátis e segura?

**Veredito curto:** **Nexus não se paga aqui.** O artefato é um tarball de
**23 kB** (7 arquivos, zero dependências) e o perfil *mínimo* recomendado do
Nexus é **4 vCPU / 16 GiB de RAM** — o dobro da VPS inteira de produção do
Ndovu. Para o que existe hoje, a resposta certa é **não hospedar registry
nenhum**: usar npm público com *provenance* (se o SDK é para terceiros) ou o
package registry do **Gitea que já está no ar** (se é interno).

Este documento é o inventário verificado, a conta que mostra por que o Nexus não
fecha, e o passo a passo das duas trilhas que fecham.

---

## 1. O que existe hoje (verificado no repo, 2026-11-01)

| Item | Estado |
|---|---|
| Pacote npm | `@ndovu/sdk` v1.4.0, `UNLICENSED`, `private: false` |
| Tamanho publicado | **23,0 kB** tarball · 68,3 kB descompactado · **7 arquivos** |
| Entry points | `./browser`, `./next`, `./node`, `./react-native` (ESM puro) |
| Dependências | **zero** (só `peerDependencies` opcionais: `next`, `react-native`) |
| Módulo Go | `github.com/mandrecorrea5/ndovu/sdk/go` — módulo **aninhado** em `sdk/go/` |
| Publicado no npm? | **Não.** `npm view @ndovu/sdk` → `E404` |
| Instalável via npm hoje? | **Não.** `npm i github:mandrecorrea5/ndovu` → `ENOENT package.json` (não há `package.json` na raiz) |
| Instalável via `go get` numa versão? | **Não.** Ver §3.1 |
| Repo no GitHub | `mandrecorrea5/ndovu` — **público** |
| Gitea corporativo | `git.gmz.corp:30022`, org `GMZ-DEVs/ndovu` — **não resolve fora da rede GMZ** |
| CI | GitHub Actions (`on: ubuntu-latest`): `ci.yml` roda `check` em `sdk` e testes em `sdk/go`; `release.yml` publica **imagens** no GHCR |
| Job de publish npm | existe só em `sdk/.github/workflows/ci.yml` — é **template** para um futuro repo `ndovu-sdk` (roda `npm publish` na raiz, onde não há `package.json`) |

**Conclusão do inventário:** hoje o SDK **não é distribuível por canal nenhum**.
Quem integra só consegue copiar o arquivo à mão (`docs/INTEGRATION.md`, "Opção
A") — que é o que a documentação recomenda. O "registry interno" aparece em
`docs/openshift/*` como intenção, nunca como implementação.

---

## 2. A decisão que vem antes da ferramenta

Nenhuma escolha de registry faz sentido antes de responder duas perguntas. Elas
mudam a resposta por completo:

**Pergunta 1 — quem instala?**
- (a) só times internos do Ndovu → registry privado resolve
- (b) empresas-cliente, no frontend delas → precisa ser alcançável **fora** da
  sua rede, com credencial por cliente
- (c) público em geral / open source → npm público

**Pergunta 2 — o código é segredo?**
Hoje o repo `mandrecorrea5/ndovu` é **público** e o `package.json` diz
`UNLICENSED`. As duas coisas se contradizem. Vale saber o que um registry
privado protege e o que **não** protege:

- **Protege:** a *autoridade de publicar*. Só quem tem token publica `@ndovu/sdk`.
- **Não protege:** o código. Ele já está legível no GitHub.

E existe um risco concreto **hoje**, independente dessa decisão:

> `@ndovu` **não está registrado no npmjs.org**. Qualquer pessoa pode criar o
> escopo `@ndovu` no npm público e publicar um `@ndovu/sdk` malicioso. Quem
> instalar por engano recebe código de terceiro — *dependency confusion* clássico.
> Publicar no npm público (mesmo que só com um placeholder) é o que **fecha** essa
> porta. Um registry privado não fecha: ele não impede ninguém de registrar o
> escopo no npm público.

Se a resposta da pergunta 2 for "o SDK é proprietário, o repo público é
acidental", o primeiro movimento é **fechar o repo**, não montar registry.

---

## 3. Lacunas a corrigir antes de qualquer publish

Estas são bloqueios reais, na ordem em que quebram.

### 3.1 O módulo Go não tem versão válida (verificado)

O módulo vive em `sdk/go/`, ou seja, é um **módulo aninhado**. Go exige tag com
prefixo do diretório (`sdk/go/v1.4.0`). O repo tem só `v1.3.0`, que é a tag do
módulo **raiz**. Resultado medido:

```console
$ go get github.com/mandrecorrea5/ndovu/sdk/go@v1.3.0
go: downloading github.com/mandrecorrea5/ndovu v1.3.0
go: github.com/mandrecorrea5/ndovu/sdk/go@v1.3.0: verifying module:
    reading https://sum.golang.org/lookup/github.com/mandrecorrea5/ndovu@v1.3.0: 404 Not Found

$ go get github.com/mandrecorrea5/ndovu/sdk/go@main
go: added github.com/mandrecorrea5/ndovu/sdk/go v0.0.0-20261029041038-7f79e3f2e49b
```

Ou seja: só `@main` funciona, e o consumidor fica preso a um pseudo-versão
`v0.0.0-<data>-<sha>` — sem semver, sem `go get -u`, sem garantia de tag. O
`README` promete `go get .../sdk/go` puro, que hoje falha.

**Correção:** criar tag `sdk/go/v1.4.0` (e `sdk/go/v1.3.0` retroativamente, se
quiser recuperar a 1.3.0 com a API correta). Com repo público e tag correta, o
`proxy.golang.org` e o `sum.golang.org` funcionam sem nenhuma infraestrutura
extra — **Go não precisa de Nexus nem de Gitea** se o repo é público.

### 3.2 Caminho do módulo inconsistente

`sdk/go/go.mod` diz `github.com/mandrecorrea5/ndovu/sdk/go`; `backend/go.mod`
diz `github.com/marcoscorrea/ndovu/backend` (sem o `5`). Um dos dois aponta para
uma conta que provavelmente não existe. Escolher o canônico **agora** importa:
renomear módulo Go depois é *breaking change* para todo consumidor.

### 3.3 `package.json` sem o que o publish exige

| Campo | Estado | Por quê importa |
|---|---|---|
| `repository` | **ausente** | `--provenance` **exige** esse campo apontando para o repo de origem, com match case-sensitive |
| `publishConfig` | ausente | sem ele o publish vai para o registry default do `~/.npmrc` de quem roda — resultado não determinístico |
| `license` | `UNLICENSED` | npm publica assim; o consumidor não tem licença de uso declarada |
| `.npmrc` | **não existe** | nenhum alvo de publish está definido no repo |

### 3.4 Versão diverge da tag

`package.json` em **1.4.0**, última tag em **v1.3.0**. Um `npm publish` hoje
publicaria 1.4.0 sem tag correspondente — e a pipeline de publish dispara em
`refs/tags/v*`, então ela nunca rodou de fato.

### 3.5 O job de publish não aponta para o pacote

`sdk/.github/workflows/ci.yml` roda `npm publish` na raiz do repo. Isso só
funciona no repo separado `ndovu-sdk` (que a doc descreve mas não existe). No
monorepo, faltaria `working-directory: sdk`. Enquanto o split não acontecer,
publish tem que ser no `ci.yml` da raiz com `working-directory`.

---

## 4. As opções, lado a lado

| | Nexus CE | Gitea Packages | Verdaccio | GitHub Packages | npm público |
|---|---|---|---|---|---|
| Licença/custo | grátis (CE) | grátis (self-host) | grátis (MIT) | grátis p/ pacote **público** | grátis |
| Já existe no Ndovu? | não | **sim** (`git.gmz.corp`) | não | **sim** (`release.yml` → GHCR) | — |
| RAM mínima realista | **16 GiB** (perfil XS) | compartilhada c/ Gitea | ~200–400 MB | 0 (SaaS) | 0 |
| Formatos | npm, Maven, PyPI, Docker, Go*, generic | npm, Go, generic, Docker, Maven, PyPI… | **só npm** | **só npm** (Go = git tag) | npm |
| Auth do consumidor | token do Nexus | PAT `package` scope | token/plugin | **PAT obrigatório, até p/ público** | nenhuma (público) |
| Provenance / assinatura | não | não | não | não | **sim (Sigstore)** |
| Acessível a cliente externo | se exposto | **não** (VPN GMZ) | se exposto | sim | sim |
| Superfície que você patcharia | Java 21 + Postgres + proxy | já patchada por outro time | Node + proxy | nenhuma | nenhuma |

\* Nexus tem suporte a Go proxy, mas o Go funciona nativamente via `GOPROXY`
público para repo público — é infraestrutura redundante.

### 4.1 Por que o Nexus não fecha a conta

Números do próprio fornecedor ([system requirements](https://help.sonatype.com/en/sonatype-nexus-repository-system-requirements.html)):

| Perfil | vCPU/nó | RAM/nó | Throughput de referência |
|---|---|---|---|
| Extra-Small (XS) | 4 | **16 GiB** | 100 rps (picos 150–200) |
| Small (S) | 8 | 32 GiB | 500 rps |

Contra a realidade do Ndovu (`docs/CAPACITY-VPS.md`): **4 vCPU · 8 GB · 200 GB**,
com a soma dos `limits` de todos os serviços já em **8,0 GB** e `swap = 0`. Não
há 16 GiB para dar, nem 4 vCPU sobrando: o próprio documento diz que o primeiro
sintoma de falta de CPU é *steal* do hypervisor.

Agravantes que aparecem só depois de subir:

- **Java 21** obrigatório — mais uma runtime para patch e CVE.
- **H2 embutido + container não é suportado.** Para rodar em Docker, precisa de
  **Postgres externo** — ou seja, o registry passa a ter banco, backup e restore.
- **CE tem teto:** 40.000 componentes e 100.000 requests/dia. Para 7 arquivos é
  folgado, mas é um teto comercial a monitorar.
- **Cada release tem 1 ano de suporte**; versão fora da janela é "sunset" — você
  assume o ciclo de upgrade de um app Java exposto.
- Histórico de CVE crítico no produto (path traversal, etc.) faz dele um alvo
  sensível se ficar exposto na internet.

**Conta honesta:** subir Nexus "só para o SDK" custa uma VPS *maior que a de
produção* (16 GiB ≈ US$ 60–100/mês), mais Postgres, proxy, backup, monitoramento
e ciclo de patch — para servir **23 kB**. Isso é ~50–100× o custo da VPS inteira
do Ndovu por um artefato que cabe num e-mail.

**Quando o Nexus passaria a fazer sentido:** várias equipes consumindo Maven +
PyPI + Docker + npm, com cache de proxy para economizar banda, política de
licença e quarentena de malware. Aí o produto é *Pro+*, não CE — e a conversa é
outra (governança de supply chain, não "onde publico meu SDK"). Nada disso
existe no Ndovu hoje.

### 4.2 Por que o Gitea é o "grátis e seguro" mais próximo

O Gitea tem package registry nativo ([npm](https://docs.gitea.com/usage/packages/npm),
[Go](https://docs.gitea.com/usage/packages/go)) com o que interessa de segurança:

- pacotes **scoped** e unscoped, `publish`/`unpublish`/`dist-tag`/`view`/`search`
- auth por **PAT com escopo `package`** (não a senha da conta)
- RBAC por organização, **MFA**, audit log — coisas que o Nexus CE *não* tem
  (SAML/OIDC é só Pro)
- **já está de pé, já é patchado por outro time** — superfície nova zero
- sua versão é **rejeitada se já existir** (`409`) — bom contra sobrescrita
  acidental de release

**O bloqueio:** `git.gmz.corp` não resolve fora da rede GMZ (verificado). Serve
para time interno, **não** para empresa-cliente instalar no frontend dela. E é
infra corporativa GMZ: publicar um produto comercial `mandrecorrea5/ndovu` lá é
decisão de governança, não técnica.

Sem provenance — que exige GitHub Actions ou GitLab CI (npm CLI ≥ 9.5).

### 4.3 Por que o npm público + provenance é a opção mais segura de todas

Parece contraintuitivo, mas é a que **menos superfície de ataque você possui**:

- **Zero servidor para patch.** O registry é da npm; CVE de registry não é seu.
- **Provenance Sigstore:** atestado assinado ligando pacote → commit → workflow,
  registrado em log de transparência imutável. O consumidor verifica com
  `npm audit signatures`. Nenhuma das alternativas self-hosted entrega isso.
- **Trusted publishing (OIDC):** dispensa `NPM_TOKEN` de longa duração — o
  workflow troca um token efêmero por OIDC. Elimina o segredo mais roubável do
  repo (hoje o `ci.yml` do SDK usa `NPM_TOKEN` estático).
- **Fecha o sequestro de escopo** (§2).
- Custo: **US$ 0**, inclusive tráfego.

Custo real: o pacote fica público. Se o SDK é para frontends de cliente, isso é
*feature*, não problema — o artefato é servido ao navegador do cliente de
qualquer jeito, e o segredo do Ndovu é a API key, não o SDK (o próprio
`sdk/README.md` diz: "a chave vai para o bundle do frontend, então é pública por
natureza").

---

## 5. Recomendação

### Trilha A — SDK é para cliente externo ou vai ser aberto (recomendada)

**npm público + provenance + trusted publishing.**

1. Fechar as lacunas de §3.1–3.5 (tag `sdk/go/v1.4.0`, caminho de módulo
   canônico, `repository`/`publishConfig`/`license`).
2. Claim do escopo `@ndovu` no npmjs.org + **2FA obrigatório** na org.
3. Trusted publishing (OIDC) no GitHub Actions, sem `NPM_TOKEN`:
   `npm publish --provenance --access public`.
4. Consumidor: `npm i @ndovu/sdk` — sem token, sem `.npmrc`.
5. Go: tag `sdk/go/v1.4.0` no repo público → `go get .../sdk/go@v1.4.0`
   funciona via proxy público. Nenhum registry necessário.
6. CI semanal: `npm audit signatures` + `npm view @ndovu/sdk` para detectar
   versão inesperada.

### Trilha B — SDK é proprietário e só time interno consome

**Gitea Packages** (custo zero, já no ar), com o repo fechado.

```bash
# publicar
npm config set @ndovu:registry https://git.gmz.corp/api/packages/GMZ-DEVs/npm
npm config set -- '//git.gmz.corp/api/packages/GMZ-DEVs/npm/:_authToken' "$GITEA_PAT"
npm publish            # 409 se a versão já existe — é o comportamento correto

# consumir
npm config set @ndovu:registry https://git.gmz.corp/api/packages/GMZ-DEVs/npm
npm config set -- '//git.gmz.corp/api/packages/GMZ-DEVs/npm/:_authToken' "$GITEA_PAT_READONLY"
```

PAT de leitura separado do de escrita, um por consumidor quando houver cliente
externo. Para Go: `GOPROXY=https://git.gmz.corp/api/packages/GMZ-DEVs/go` +
credencial.

Se um dia precisar de endpoint **fora da VPN** com auth: **Verdaccio** numa VPS
pequena (200–400 MB de RAM, MIT) atrás do Caddy/Cloudflare Tunnel que vocês já
operam — não o Nexus. E mantenha `publish` sempre autenticado: registry npm
aberto para escrita é execução remota de código disfarçada.

> **Não use Cloudflare Access na frente do registry npm.** O service token do
> Access usa os headers `CF-Access-Client-Id`/`CF-Access-Client-Secret`, que o
> npm **não** consegue enviar (ele só manda `Authorization` via `_authToken`).
> Deixe a autenticação no próprio registry e use a Cloudflare só para TLS/WAF.

### O que **não** fazer

- ❌ Subir Nexus na VPS de produção do Ndovu: não há RAM (§4.1) e o OOM killer
  levaria o Postgres junto (a ingestão para inteira).
- ❌ Subir Nexus numa VPS nova só para 23 kB: 16 GiB para servir 7 arquivos.
- ❌ Publicar num registry com `publish` anônimo.
- ❌ Deixar `@ndovu` livre no npmjs.org: é o risco mais concreto da lista.

---

## 6. Checklist antes do primeiro publish

- [ ] Decidir visibilidade do SDK e **alinhar com a visibilidade do repo**
- [ ] Registrar o escopo `@ndovu` no npmjs.org (ou aceitar o risco por escrito)
- [ ] `repository` no `package.json` (obrigatório p/ provenance)
- [ ] `publishConfig: { access: "public" }` + registry explícito
- [ ] `license` decidido (`UNLICENSED` é uma escolha válida, mas declarada)
- [ ] Tag `sdk/go/v1.4.0` criada; `go get @v1.4.0` testado num módulo limpo
- [ ] Caminho de módulo Go canônico (hoje `mandrecorrea5` vs `marcoscorrea`)
- [ ] `package.json` e tag na **mesma** versão
- [ ] `working-directory: sdk` no job de publish (ou split do repo `ndovu-sdk`)
- [ ] Token de publish trocado por OIDC/trusted publishing
- [ ] `npm pack --dry-run` revisado: 7 arquivos, 23 kB — nada de `.env` no tarball

---

## 7. Respostas diretas

**"Como subir os SDKs em algum lugar, tipo um Nexus?"**
Não precisa de Nexus. Para o público: `npm publish --provenance` no npmjs.org.
Para interno: `npm publish` no Gitea que já existe. Go: tag `sdk/go/v1.4.0` no
repo. Antes disso, corrigir §3 — hoje o pacote não é instalável por canal nenhum.

**"Vale a pena baixar e subir o Nexus numa VPS?"**
Não, neste estágio. O perfil mínimo é 16 GiB de RAM; a VPS de produção tem 8 GB
com os limites já no teto. O custo seria uma VPS maior que a de produção, mais
Postgres, backup e patch de Java 21 — para servir 23 kB. Nexus se justifica com
vários formatos (Maven/PyPI/Docker), várias equipes e política de licença: aí é
Pro+, outro orçamento e outro problema.

**"Tem solução grátis que faça o mesmo, mas segura?"**
Duas, e ambas são mais seguras que o Nexus aqui:
1. **npm público + provenance Sigstore + trusted publishing** — grátis, zero
   servidor para patch, atestado verificável, e fecha o sequestro do escopo
   `@ndovu`. É a resposta mais segura porque a superfície que você opera é zero.
2. **Gitea Packages** — grátis, já no ar, auth por PAT com escopo, MFA, audit
   log. Limitação: VPN GMZ, então só para time interno.
   Verdaccio (MIT, ~300 MB) é o meio-termo se precisar de endpoint externo com
   auth — nunca o Nexus.

---

### Fontes consultadas

- [Nexus Repository — System Requirements](https://help.sonatype.com/en/sonatype-nexus-repository-system-requirements.html) (perfis XS–XL, Java 21, limites do H2)
- [Nexus Repository — Editions and Feature Availability](https://help.sonatype.com/en/nexus-repository-tiers-and-feature-availability.html) (CE grátis, teto de 40k componentes / 100k req/dia)
- [Nexus Repository 3 — Versions Status](https://help.sonatype.com/en/sonatype-nexus-repository-3-versions-status.html) (suporte de 1 ano por release)
- [Gitea — NPM Package Registry](https://docs.gitea.com/usage/packages/npm) · [Go Package Registry](https://docs.gitea.com/usage/packages/go)
- [Verdaccio — What is Verdaccio](https://verdaccio.org/docs/what-is-verdaccio)
- [GitHub Packages — npm registry](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-npm-registry) (token obrigatório até p/ público) · [billing](https://docs.github.com/en/billing/concepts/product-billing/github-packages) (grátis p/ pacotes públicos)
- [npm — Generating provenance statements](https://docs.npmjs.com/generating-provenance-statements) (GitHub Actions/GitLab, npm CLI ≥ 9.5, trusted publishing)
