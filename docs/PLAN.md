# devreclaim — Dossier de développement complet

> Nettoyeur de disque open source pour développeurs, pensé d'abord pour **Windows + WSL2 + Docker Desktop**.
> Nom : `devreclaim` (libre sur GitHub et winget au 2026-11-25 ; scoop et domaine restent à vérifier).
> Auteur : Loïc (LoicRU) — projet solo, coût cible 0 €.
>
> **Révision 2** (2026-10-19) — intègre une relecture critique : planning réaliste, point de
> validation avant d'investir dans M2–M7, suppression `--trash` par défaut en v0.1, priorité
> renforcée sur le risque antivirus/confiance, garde-fou contre l'échec silencieux de `fstrim`.
> Les changements par rapport à la version initiale sont marqués **[RÉVISÉ]**.

Légende des preuves : **[F]** fait lu sur une page · **[O]** observation · **[H]** hypothèse à valider.

---

## 0. TL;DR

- **Problème** : le disque C: se remplit chez les devs Windows (node_modules, caches de paquets, images WSL2/Docker qui grossissent mais ne rétrécissent pas) et personne n'a d'outil open source qui voit *tout* et nettoie *sans danger*.
- **Solution** : un binaire unique `devreclaim.exe` (CLI + TUI) qui scanne en lecture seule, explique ce qui prend la place, estime ce qui est *réellement récupérable*, puis nettoie avec garde-fous.
- **Différenciateur** : un rapport unique Windows + WSL + Docker + caches + artefacts de projets, avec estimation du gain réel (pas la taille brute).
- **MVP** : **[RÉVISÉ]** 8–12 semaines réalistes (pas 4), avec un point de validation après le scanner WSL avant d'investir dans le reste. Premier livrable concret : le **scanner WSL en lecture seule** (taille du `.vhdx` vs espace réellement utilisé dans la distro).

---

## 1. Contexte et preuves de marché

| Constat | Niveau | Source |
|---|---|---|
| npkill (nettoyeur `node_modules`) : ~9,4k★ ; ne mentionne ni Windows ni WSL | [F] | https://github.com/voidcosmos/npkill |
| kondo (artefacts de build, 20+ écosystèmes) : ~2,4k★ ; Windows supporté, WSL non mentionné ; « essentially `rm -rf` with a prompt » | [F] | https://github.com/tbillington/kondo |
| Mole (nettoyeur Mac) : ~68,5k★ ; version Windows open source « expérimentale » (branche `windows`) | [F] | https://github.com/tw93/mole |
| App Windows de Mole : payante (~15 $), source privée ; images WSL seulement *expliquées* dans l'onglet d'analyse, pas nettoyées ; pas de mention de Docker/caches de paquets | [F] | https://mole.fit/windows |
| Topic `disk-cleanup` : meilleurs outils Windows < 800★ ; meilleur outil « dev » est Mac (`cleanmymac-cli`, 989★) | [F] | https://github.com/topics/disk-cleanup |
| `ext4.vhdx` de WSL2 grossit mais ne rétrécit pas ; le mode sparse est signalé comme instable | [F] | https://github.com/microsoft/WSL/issues/12103 |
| Sparse VHD : bloqué pour risque de corruption, réactivé en *expérimental* dans WSL 3.0.2 pré-release (6 oct. 2026) | [F] | https://xenospectrum.com/en/wsl302-vnuma-sparse-vhd-scope/ |
| `Optimize-VHD` exige Hyper-V (absent de Windows Famille) ; conseils erronés (`wslcompact`) circulent | [F] | https://learn.microsoft.com/en-us/answers/questions/4006845/reduce-disk-space-consumption-for-wsl |
| Docker Desktop : VHDX grossit à chaque mise à jour, pas de moyen simple de récupérer | [F] | https://forums.docker.com/t/docker-desktop-wsl2-vhdx-grows-by-1-5gb-after-every-update-no-way-to-devreclaim-space/151729 |
| Script PowerShell de compaction WSL : 112★, `.exe` non signé (faux positifs AV possibles), compaction seule | [F] | https://github.com/hyperphantasia/WSL-VHDX-Compact |
| Un outil complet mais invisible peut avoir 0★ (ex. `romnn/cooldown`, autre niche) → la découvrabilité est le vrai goulot | [O] | rapport précédent |

**Risques de marché à garder en tête**
1. Microsoft peut rendre le sparse VHD fiable → la partie « compaction WSL » perdrait de la valeur (le reste — caches, artefacts, rapport unifié — reste).
2. Mole Windows peut étendre son périmètre dev.
3. La confiance : un outil qui supprime des fichiers doit être irréprochable sur la sécurité.
4. **[RÉVISÉ]** Un `.exe` Go neuf et non signé qui touche le registre, supprime des dossiers récursivement et appelle `diskpart`/`wsl.exe` déclenchera *quasi systématiquement* une alerte heuristique SmartScreen/Defender dès les premières exécutions — ce n'est pas un risque marginal, c'est proche du résultat par défaut. Voir §10 et §16.

---

## 2. Vision, public, promesse

- **Public** : développeurs sous Windows (WSL2, Docker Desktop), étudiants (Epitech, etc.), DevOps sur portable.
- **Promesse** (une phrase pour le README) : *« See exactly what's eating your dev disk on Windows — projects, caches, WSL, Docker — and devreclaim it safely. »*
- **Principes non négociables**
  1. **Lecture seule par défaut.** Rien n'est supprimé sans `--apply` explicite.
  2. **Honnêteté des chiffres** : on distingue *taille brute*, *espace réellement récupérable* (hardlinks, vhdx) et *estimation*.
  3. **Reconstructible d'abord** : on ne propose par défaut que ce qui peut être régénéré (lockfile présent, cache de paquets…).
  4. **Déléguer aux commandes natives** quand elles existent (`npm cache clean`, `go clean`, `docker builder prune`…).
  5. **Zéro télémétrie, zéro réseau** pour scanner/nettoyer.
  6. **[RÉVISÉ]** **Réversible d'abord, tant que la confiance n'est pas établie.** En v0.1, un outil tout neuf et non signé n'a pas droit à l'erreur sur la première impression : la suppression par défaut doit être réversible (Corbeille), même si c'est plus lent. Voir §6 et §13.

---

## 3. Périmètre

### 3.1 MVP (v0.1) — indispensable
- `scan` : rapport en lecture seule, trié par taille, avec catégories.
- Artefacts de projets : `node_modules`, `target` (Rust), `.venv`/`venv`, `__pycache__`, `build`, `dist`, `.gradle`, `bin/obj` (.NET) — avec âge de dernière modification et indicateur « reconstructible ».
- Caches globaux : npm, pnpm, yarn, pip, uv, cargo registry, NuGet, Gradle, Maven, Go.
- WSL2 : liste des distros, taille du `.vhdx`, espace utilisé dans la distro, gain récupérable estimé.
- `clean` : dry-run par défaut, `--apply` pour exécuter, confirmation interactive, sortie JSON.
- Compaction WSL guidée (`devreclaim wsl compact`) avec repli `diskpart` si Hyper-V absent.
- Installation : release GitHub (`.exe` + SHA256) ; winget/scoop juste après.

### 3.1bis **[RÉVISÉ] Point de validation avant d'aller plus loin**
Avant d'investir dans 3.1 au complet (M2–M7), livrer et diffuser le scanner WSL seul
(`devreclaim wsl status`, lecture seule, pas de suppression) comme mini-outil ou article
autonome. Objectif : obtenir un signal de personnes extérieures (quelqu'un d'autre que
l'auteur l'utilise, commente, ou dit « ça m'a aidé ») avant de construire le scanner
d'artefacts, les caches, l'executor de suppression, le TUI et le pipeline de release
complet. Voir le jalon **M1.5** en §12.

### 3.2 Versions ultérieures
- Docker Desktop (`docker system df`, prune guidé, compaction du VHDX).
- Cache Visual Studio / JetBrains / VS Code.
- Planification (tâche Windows mensuelle).
- Export HTML/Markdown du rapport (partage du « avant/après »).
- Profils par écosystème, liste blanche/noire utilisateur (`devreclaim.toml`).
- Support Linux/macOS (très bas dans la priorité).

### 3.3 À ne PAS faire au départ
- Interface graphique native, IA, nettoyage du registre, « fichiers système Windows », désinstallation d'applications, suppression silencieuse, auto-update, télémétrie.

---

## 4. Spécification fonctionnelle

### 4.1 CLI

```
devreclaim scan   [--root <path>...] [--category <c>...] [--min-size 100MB] [--json] [--no-wsl] [--no-docker]
devreclaim clean  [--category <c>...] [--older-than 30d] [--apply] [--yes] [--trash|--delete] [--json]
devreclaim wsl status
devreclaim wsl compact [--distro <name>|--all] [--method auto|optimize-vhd|diskpart] [--apply]
devreclaim docker status            # v0.2
devreclaim explain <candidate-id>   # pourquoi c'est proposé, risque, commande équivalente
devreclaim config [init|show|path]
devreclaim version
```

Catégories (`--category`) : `project-artifacts`, `package-caches`, `wsl`, `docker`, `ide-caches`, `windows-temp`.

**Codes de sortie** : `0` OK · `1` erreur générique · `2` usage · `3` droits insuffisants (admin requis) · `4` opération annulée · `5` garde-fou déclenché (chemin protégé, etc.).

### 4.2 Modèle de données (cœur)

```go
type Category string // "project-artifacts" | "package-caches" | "wsl" | "docker" | ...

type Risk string // "safe" | "low" | "medium" | "high"

type Candidate struct {
    ID            string    // stable : hash(category + path/identifiant)
    Category      Category
    Kind          string    // "node_modules", "cargo-registry", "wsl-vhdx", ...
    Path          string    // vide pour les actions sans chemin (docker prune)
    Label         string    // texte lisible
    SizeBytes     int64     // taille logique
    OnDiskBytes   int64     // taille allouée (si calculable)
    ReclaimBytes  int64     // estimation du gain réel (hardlinks dédupliqués, vhdx…)
    ReclaimNote   string    // "estimé", "exact", "dépend de fstrim"
    LastModified  time.Time
    Regenerable   bool
    RegenHint     string    // ex. "npm ci", "cargo build"
    Risk          Risk
    Reasons       []string  // pourquoi proposé
    Warnings      []string  // pourquoi prudence
    Action        ActionSpec
}

type ActionSpec struct {
    Type     string   // "delete-dir" | "run-command" | "wsl-compact"
    Command  []string // pour run-command (ex. {"npm","cache","clean","--force"})
    NeedsAdmin bool
}
```

### 4.3 Sortie JSON (contrat public, versionné)

```json
{
  "schemaVersion": 1,
  "tool": "devreclaim",
  "version": "0.1.0",
  "generatedAt": "2026-10-20T10:00:00Z",
  "summary": { "totalBytes": 0, "reclaimableBytes": 0, "candidates": 0 },
  "candidates": [ { "id": "...", "category": "...", "...": "..." } ],
  "warnings": []
}
```
Ne jamais casser `schemaVersion: 1` sans incrémenter.

---

## 5. Détails techniques par scanner

> Toutes les commandes/chemins ci-dessous doivent être **vérifiés sur une vraie machine** (marqués [à vérifier]) car ils varient selon versions.

### 5.1 Artefacts de projets
- Parcours des racines (`--root`, défaut : dossiers courants de dev détectés + répertoire utilisateur, **configurable**).
- Détection par couple *dossier cible + fichier marqueur* :
  | Dossier | Marqueur requis | Reconstructible via | Remarque |
  |---|---|---|---|
  | `node_modules` | `package.json` au même niveau | `npm ci` / `pnpm i` / `yarn` (lockfile présent ⇒ reconstructible) | si pas de lockfile ⇒ risque `medium` |
  | `target` | `Cargo.toml` | `cargo build` | |
  | `.venv`, `venv` | `pyvenv.cfg` dans le dossier | recréation via requirements/pyproject | |
  | `bin`, `obj` | `*.csproj` / `*.sln` | `dotnet build` | |
  | `build`, `dist` | marqueur de projet + absent de git (ignoré) | rebuild | risque `medium` : ne pas supprimer si suivi par git |
  | `.gradle`, `build` | `build.gradle(.kts)` | gradle | |
  | `__pycache__` | — | automatique | risque `safe` |
- **Ne jamais descendre dans** un `node_modules` trouvé (on le compte, on ne le parcourt pas pour chercher d'autres projets), ni suivre les **jonctions/symlinks (reparse points)**.
- Âge : dernière modification du *projet* (pas du dossier cible). Par défaut, ne proposer que si inactif depuis ≥ 30 jours (configurable) ; sinon avertissement.
- **Piège hardlinks** : pnpm/uv utilisent des liens physiques ⇒ supprimer `node_modules` libère moins que sa taille. Calculer `ReclaimBytes` en dédupliquant par *file ID* (Windows : `GetFileInformationByHandle` / `FileIdInfo`) et en ignorant les fichiers ayant des liens vers le store conservé. **[RÉVISÉ]** Ne pas viser l'exactitude globale (ça supposerait de connaître toutes les références au même inode sur tout le disque, ce qui coûte un parcours complet) : afficher la taille logique brute et marquer `ReclaimNote: "estimé, peut être inférieur (store partagé pnpm/uv)"` de façon bien visible dans la sortie CLI, pas seulement dans les métadonnées JSON.
- **Piège OneDrive** : fichiers « cloud-only » (placeholders) — ne pas les déclencher en lecture ; les ignorer ou les marquer.
- **Chemins longs** : utiliser le préfixe `\\?\` à la suppression.
- **Fichiers en lecture seule** : retirer l'attribut avant suppression (cas fréquent dans `.git`, `node_modules`).
- **Performance** : parcours concurrent borné (worker pool, `runtime.NumCPU()*2`), `os.ReadDir` plutôt que `filepath.Walk` si possible, annulation par `context`.

### 5.2 Caches de paquets (privilégier les commandes natives)
| Cache | Détection du chemin | Nettoyage |
|---|---|---|
| npm | `npm config get cache` | `npm cache clean --force` |
| pnpm | `pnpm store path` | `pnpm store prune` |
| yarn | `yarn cache dir` | `yarn cache clean` |
| pip | `pip cache dir` | `pip cache purge` |
| uv | `uv cache dir` | `uv cache clean` |
| cargo | `%USERPROFILE%\.cargo\registry` (+ `git`) | supprimer (ou `cargo cache` si installé) |
| NuGet | `dotnet nuget locals all --list` | `dotnet nuget locals all --clear` |
| Gradle | `%USERPROFILE%\.gradle\caches` | supprimer sous-dossiers anciens (prudence : `medium`) |
| Maven | `%USERPROFILE%\.m2\repository` | supprimer (reconstructible, long à refaire ⇒ `medium`) |
| Go | `go env GOCACHE` / `go env GOMODCACHE` | `go clean -cache` / `go clean -modcache` |

Règle : si l'outil n'est pas installé (commande introuvable), on lit le chemin par défaut mais on **ne propose qu'une suppression de dossier** avec `Risk` adapté.

### 5.3 WSL2 (le cœur différenciant)
**Détection des distros**
- Registre : `HKCU\Software\Microsoft\Windows\CurrentVersion\Lxss\{GUID}` → `DistributionName`, `BasePath` (peut commencer par `\\?\`), `Version`, `Flavor` [à vérifier].
- VHDX : `<BasePath>\ext4.vhdx`. Distros Microsoft Store : sous `%LOCALAPPDATA%\Packages\<pkg>\LocalState\ext4.vhdx` [à vérifier].
- Complément : `wsl -l -v` (attention à l'encodage UTF-16LE de la sortie de `wsl.exe` : décoder explicitement).

**Mesure utile**
- Taille du fichier `.vhdx` (sur disque).
- Espace utilisé *dans* la distro : `wsl -d <distro> -u root -- df -B1 /` (ne démarre la distro que si nécessaire ; signaler que ça la démarre).
- `Gain estimé ≈ taille_vhdx − espace_utilisé_dans_distro` (borne haute, annoncée comme *estimation*).

**Compaction (action sensible)**
1. Pré-vérifs : sauvegarde conseillée, espace libre suffisant, droits admin.
2. `fstrim -av` dans la distro (`wsl -d <d> -u root -- fstrim -av`) — peut échouer : avertir et continuer.
3. `wsl --shutdown`.
4. Méthode `auto` : si le module Hyper-V est dispo ⇒ `Optimize-VHD -Path <vhdx> -Mode Full` ; sinon `diskpart` :
   ```
   select vdisk file="<chemin>"
   attach vdisk readonly
   compact vdisk
   detach vdisk
   ```
   Toujours `detach vdisk` en `defer`/finally, même sur erreur (sinon fichier verrouillé).
5. Mesurer avant/après, afficher le gain réel.
   **[RÉVISÉ] Garde-fou anti-échec-silencieux** : si le gain mesuré est faible (ex. < 5–10 % de la taille avant compaction), ne pas simplement afficher les octets bruts — afficher un message explicite du type *« compaction exécutée, mais peu d'espace récupéré — `fstrim` n'a probablement pas pu libérer assez de blocs »*. C'est la fonctionnalité vitrine du projet ; un gain proche de zéro affiché comme un succès neutre donnera l'impression que l'outil est cassé, en particulier dans les démos/captures du README.
- Gérer le cas sparse : si `sparse` activé, certaines étapes diffèrent (le fil WSL#12103 décrit : désactiver sparse → `Optimize-VHD` → réactiver). **Ne pas activer/désactiver sparse automatiquement en v0.1** : détecter et expliquer.
- `wsl.exe` en UTF-16 ⇒ `golang.org/x/text/encoding/unicode`.

### 5.4 Docker Desktop (v0.2)
- `docker system df --format json` (images, conteneurs, volumes, build cache).
- Actions proposées : `docker builder prune`, `docker image prune`, `docker container prune` (jamais `volume prune -a` sans avertissement fort : perte de données).
- VHDX Docker Desktop : chemin à détecter [à vérifier], souvent `%LOCALAPPDATA%\Docker\wsl\disk\docker_data.vhdx` (ou ancien `...\data\ext4.vhdx`) ; compaction après arrêt de Docker Desktop.

---

## 6. Sécurité des suppressions (garde-fous)

1. **Liste de chemins protégés** (jamais supprimés, quoi qu'il arrive) : racine de lecteur, `C:\Windows`, `C:\Program Files*`, `C:\Users\<u>` (racine), `Documents`, `Desktop`, `Pictures`, dossiers `.git`, `.ssh`, `.gnupg`, tout ce qui est hors des racines de scan.
2. **Validation avant action** : le chemin est un descendant strict d'une racine autorisée, existe, n'est pas un reparse point, et correspond toujours au type attendu (re-vérifier le marqueur juste avant suppression — TOCTOU).
3. **Dry-run par défaut** ; `--apply` requis ; confirmation interactive sauf `--yes`.
4. **Journal** : chaque suppression écrite dans `%LOCALAPPDATA%\devreclaim\logs\YYYY-MM-DD.jsonl` (chemin, taille, résultat).
5. **[RÉVISÉ] Corbeille/quarantaine** : `--trash` est le **défaut en v0.1**, même si plus lent sur de gros `node_modules`. `--delete` reste disponible pour qui le demande explicitement. Un outil neuf et non signé n'a pas droit à l'erreur sur la première impression ; la réversibilité prime sur la vitesse jusqu'à ce qu'une réputation soit établie (voir §13, décision révisée).
6. **Pas d'élévation silencieuse** : si l'admin est requis (compaction), le dire et s'arrêter avec le code `3` plutôt que d'auto-élever.
7. **Pas de suppression de fichiers verrouillés par un processus actif** sans l'indiquer (erreur claire, continuer avec les autres).
8. **[RÉVISÉ] Jamais demander à l'utilisateur d'ajouter une exclusion Windows Defender.** C'est un pattern que les malwares détournent pour désactiver la détection ; même en cas de faux positif gênant, documenter le SHA256/l'attestation plutôt que de suggérer une exclusion — voir §10.
9. **Tests dédiés** aux garde-fous (voir §9).

---

## 7. Architecture

```
devreclaim/
├─ cmd/devreclaim/main.go            # point d'entrée (cobra)
├─ internal/
│  ├─ cli/                        # commandes cobra (scan, clean, wsl, config…)
│  ├─ core/
│  │   ├─ candidate.go            # types (Candidate, ActionSpec…)
│  │   ├─ scanner.go              # interface Scanner + registry
│  │   ├─ planner.go              # candidats → plan d'actions (filtres, tri)
│  │   └─ executor.go             # exécution + journal + garde-fous
│  ├─ scanners/
│  │   ├─ projects/               # artefacts de projets
│  │   ├─ pkgcaches/              # caches npm/pip/cargo/…
│  │   ├─ wsl/                    # distros, vhdx, compaction
│  │   └─ docker/                 # v0.2
│  ├─ platform/
│  │   ├─ windows/                # file IDs, reparse points, registre, corbeille
│  │   └─ exec/                   # CommandRunner (interface mockable)
│  ├─ safety/                     # chemins protégés, validations
│  ├─ report/                     # rendu texte, JSON, (HTML plus tard)
│  ├─ tui/                        # bubbletea (v0.1 minimal ou v0.2)
│  └─ config/                     # devreclaim.toml
├─ testdata/                      # arborescences de fixtures
├─ docs/                          # guides, FAQ, sécurité
├─ .github/workflows/             # ci.yml, release.yml, codeql/govulncheck
├─ .goreleaser.yaml
├─ LICENSE  README.md  CONTRIBUTING.md  SECURITY.md  CODE_OF_CONDUCT.md  CHANGELOG.md
└─ go.mod
```

**Interfaces clés**

```go
type Scanner interface {
    Name() string
    Category() Category
    Scan(ctx context.Context, opts ScanOptions) (<-chan Candidate, error)
}

type CommandRunner interface {          // permet de mocker wsl.exe, docker, npm…
    Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
}

type Filesystem interface {             // permet de tester sans toucher au vrai disque
    ReadDir(path string) ([]fs.DirEntry, error)
    Stat(path string) (fs.FileInfo, error)
    RemoveAll(path string) error
    IsReparsePoint(path string) (bool, error)
    FileID(path string) (uint64, uint64, error)
}
```

**Flux** : `scan` → chaque `Scanner` émet des `Candidate` → `planner` filtre/trie → `report` affiche. `clean` = `scan` + `planner` + `executor` (garde-fous → action → journal).

---

## 8. Choix technologiques

| Besoin | Choix | Justification |
|---|---|---|
| Langage | **Go** [H] | binaire statique unique, cross-compilation simple, bon pour I/O concurrent. Apprentissage nécessaire mais justifié — **[RÉVISÉ]** budgeter explicitement ce temps d'apprentissage dans M0 plutôt que de l'absorber dans l'estimation (voir §12) |
| CLI | `spf13/cobra` | standard, sous-commandes |
| TUI | `charmbracelet/bubbletea` + `lipgloss` + `bubbles` | écosystème mature, beau rendu |
| Windows API | `golang.org/x/sys/windows`, `golang.org/x/sys/windows/registry` | registre, handles, file IDs |
| Encodage | `golang.org/x/text` | sortie UTF-16 de `wsl.exe` |
| Config | `pelletier/go-toml/v2` | `devreclaim.toml` |
| Tests | `testing` + `testify` | assertions lisibles |
| Release | `goreleaser` | binaires + checksums + attestation |
| Lint | `golangci-lint`, `govulncheck` | qualité + vulnérabilités |
| Démo GIF | `charmbracelet/vhs` | GIFs reproductibles pour le README |

**Alternative** : Python + Textual, packagé avec PyInstaller. Plus familier (profil de Loïc) mais exécutable plus lourd et plus de faux positifs AV [H]. Rester sur Go sauf blocage — le choix est le bon vu l'objectif de distribution en binaire, mais ne pas sous-estimer la courbe d'apprentissage dans le planning (§12).

---

## 9. Tests

- **Unitaires** : chaque scanner avec un faux `Filesystem` et un faux `CommandRunner`.
- **Fixtures** (`testdata/`) : mini-projets Node/Rust/Python/.NET (avec/sans lockfile), cache fictif.
- **Golden files** : sortie JSON et texte comparée à des fichiers de référence.
- **Tests de sécurité (obligatoires, bloquants en CI)** :
  - un chemin protégé n'est jamais supprimé ;
  - une jonction/symlink pointant hors racine n'est pas suivie ;
  - un chemin modifié entre scan et suppression est rejeté (TOCTOU) ;
  - `--apply` absent ⇒ aucune écriture (vérifier avec un FS espion).
- **Hardlinks** : test que `ReclaimBytes` n'est pas gonflé par des liens physiques.
- **WSL** : sorties `wsl -l -v` / `df` enregistrées (y compris UTF-16) et rejouées via mock.
- **CI** : matrice `windows-latest` (principal) + `ubuntu-latest` (logique pure).
- **Tests manuels** (checklist avant chaque release) : VM Windows 11 avec WSL2, Docker Desktop, gros `node_modules`, chemins > 260 caractères, fichiers en lecture seule, Windows Famille (sans Hyper-V).

---

## 10. CI/CD, release, sécurité de la chaîne

- **ci.yml** : `go vet`, `golangci-lint`, `go test ./...`, `govulncheck`, build Windows.
- **release.yml** : sur tag `v*` → GoReleaser → binaires `windows/amd64` + `windows/arm64`, `checksums.txt` (SHA256), **attestation de build** (provenance), notes de release générées depuis CHANGELOG.
- **Dépendances** : Dependabot **avec délai de maturité (cooldown)** ; actions GitHub **épinglées par SHA** ; audit des workflows avec `zizmor` ; `go.sum` commité.
- **[RÉVISÉ] Faux positifs antivirus / confiance** : ce n'est pas un risque de queue de distribution, c'est proche du résultat par défaut pour un `.exe` neuf et non signé qui touche le registre, supprime récursivement et appelle `diskpart`/`wsl.exe`. Conséquences concrètes :
  - **Monter winget/scoop en priorité dès le lancement** (pas « juste après ») : les deux pré-valident le binaire et évitent le flux « télécharger depuis le navigateur puis exécuter », qui est le chemin le plus exposé à SmartScreen.
  - **Étudier la signature de code comme une vraie ligne de budget dès M0/M7**, pas comme une entrée de FAQ à traiter plus tard (certificat payant classique, ou un programme de signature gratuite pour l'open source si on en trouve un vérifié et légitime).
  - **Ne jamais suggérer d'exclusion Windows Defender** à l'utilisateur en cas de faux positif (voir §6.8) — documenter SHA256 + attestation + lien vers le code source à la place.
- **Distribution** : release GitHub → **winget** (PR dans `microsoft/winget-pkgs`) → **scoop** (bucket perso) → `go install`.
- **SECURITY.md** : canal de signalement privé (GitHub Security Advisories).

---

## 11. Documentation à écrire

1. **README** : promesse en 1 phrase, GIF « avant/après », installation en 1 commande, 3 exemples, avertissement de sécurité, comparaison honnête (npkill, kondo, Mole).
2. `docs/what-gets-deleted.md` : table exacte de ce que chaque catégorie propose + risque + comment régénérer.
3. `docs/wsl.md` : pourquoi le VHDX ne rétrécit pas, sparse vs compaction, quand utiliser quoi, prérequis (admin, Hyper-V ou diskpart).
4. `docs/safety.md` : garde-fous, journal, comment annuler.
5. `docs/faq.md` : faux positifs AV, Windows Famille, chemins longs, OneDrive, pourquoi la Corbeille par défaut.
6. `CONTRIBUTING.md` : « ajouter un scanner en 30 minutes » (interface `Scanner`, fixtures, test).
7. `CHANGELOG.md` (Keep a Changelog), `ROADMAP.md`.

**Squelette README**
```
# devreclaim
One-line promise.
[GIF]
## Install   (winget / scoop / releases)
## Quick start   devreclaim scan  →  devreclaim clean --apply
## What it finds   (tableau)
## Safety   (dry-run, protected paths, log)
## WSL & Docker   (lien docs/wsl.md)
## Compared to   (npkill, kondo, Mole — honnête)
## Roadmap / Contributing / License
```

---

## 12. Plan de développement (jalons) **[RÉVISÉ]**

> Hypothèse : 2–4 h/jour. **Estimation révisée à 8–12 semaines** (pas 4) : sans expérience Go
> préalable, M2 (dédup hardlinks via API Windows, détection reparse points, parcours concurrent)
> et M5 (diskpart attach/compact/detach avec nettoyage garanti, repli Hyper-V) sont chacun des
> tâches de plusieurs jours sur des API système, même en étant à l'aise en Go. Chaque jalon a un
> critère d'acceptation vérifiable.

**M0 — Fondations + apprentissage Go (3–5 jours, [RÉVISÉ] temps d'apprentissage inclus)**
- Vérifier le nom ; créer le dépôt ; licence MIT ; squelette Go + cobra + CI minimale.
- **[RÉVISÉ]** Budgeter explicitement 1–2 jours de prise en main de Go (syntaxe, modules, goroutines/channels de base) si ce n'est pas déjà fait — ne pas l'absorber silencieusement dans M1.
- ✅ `devreclaim version` fonctionne ; CI verte sur Windows.

**M1 — Cœur + scanner WSL lecture seule (4–6 jours)** ← *première chose à construire*
- Types `Candidate`, interface `Scanner`, `CommandRunner` mockable, rapport texte + JSON.
- Scanner WSL : registre → distros → taille `.vhdx` → `df` → estimation.
- ✅ `devreclaim wsl status` affiche, pour chaque distro, taille VHDX / utilisé / récupérable estimé, sur une vraie machine.

**M1.5 — [RÉVISÉ] Point de validation (quelques jours, en parallèle de la suite)**
- Diffuser `devreclaim wsl status` seul (petit outil ou article de blog) avant d'investir dans M2–M7.
- ✅ au moins un signal externe réel (quelqu'un d'autre que l'auteur l'utilise, ouvre une issue, ou confirme que le chiffre affiché lui est utile) avant de continuer sans réserve sur M2.
- Si le signal est nul après une diffusion sérieuse : reconsidérer l'ampleur du reste du plan plutôt que d'enchaîner M2–M7 à l'aveugle.

**M2 — Artefacts de projets (6–8 jours, [RÉVISÉ] durée augmentée)**
- Parcours concurrent, marqueurs, âge, détection reparse points, dédup hardlinks.
- ✅ `devreclaim scan --category project-artifacts` correct sur 3+ projets réels ; tests fixtures verts ; temps de scan raisonnable sur un gros dossier.

**M3 — Caches de paquets (3–4 jours)**
- Détection de chemins via commandes natives + repli par défaut.
- ✅ rapport des caches npm/pnpm/pip/uv/cargo/NuGet/Go/Gradle/Maven.

**M4 — Nettoyage sûr (5–7 jours, [RÉVISÉ] `--trash` par défaut)**
- `safety`, `planner`, `executor`, journal, dry-run/`--apply`/`--yes`, `--trash` par défaut / `--delete` en option explicite.
- ✅ tous les tests de sécurité passent ; aucune écriture sans `--apply` ; log JSONL produit.

**M5 — Compaction WSL (4–6 jours, [RÉVISÉ] garde-fou anti-échec-silencieux inclus)**
- fstrim → shutdown → Optimize-VHD/diskpart → mesure avant/après ; `detach` garanti ; avertissement explicite si le gain mesuré est faible (voir §5.3).
- ✅ gain réel mesuré sur une VM ; Windows Famille (diskpart) testé ; le cas « gain faible » affiche un message clair et non un faux succès.

**M6 — TUI minimale + finitions (3–5 jours)**
- Liste triable, cases à cocher, résumé « X Go récupérables ».
- ✅ démo GIF enregistrée avec VHS.

**M7 — Release 0.1 (3–4 jours, [RÉVISÉ] winget/scoop priorisés)**
- GoReleaser, attestation, README, docs, 8–10 issues « good first issue ».
- **[RÉVISÉ]** Préparer les PR winget/scoop en parallèle de la release GitHub, pas « juste après » — c'est le chemin de distribution qui évite le plus de faux positifs AV (voir §10).
- ✅ release publique `v0.1.0`, installation vérifiée sur machine propre, idéalement déjà disponible ou en cours de revue sur winget/scoop.

**Total [RÉVISÉ]** ≈ 8–12 semaines (M0–M7), avec le point de validation M1.5 comme garde-fou pour ne pas investir à l'aveugle dans M2–M7 [H].

### Issues « good first issue » à préparer
Ajouter un scanner pour : Bun cache, Deno cache, Composer, pub (Dart), Unity Library, Unreal DerivedDataCache, `.terraform`, Poetry cache, conda pkgs, VS Code extensions obsolètes.

---

## 13. Décisions ouvertes (à trancher avant ou pendant M1)

1. **Nom final** (vérifier disponibilité GitHub / winget / scoop / domaine).
2. **[RÉVISÉ] Suppression par défaut** : Corbeille (lent, réversible) vs suppression directe (rapide) ? **Décision révisée : `--trash` par défaut en v0.1**, `--delete` disponible en option explicite. Raison : un outil neuf et non signé n'a pas droit à l'erreur sur la première impression ; la réversibilité prime sur la vitesse jusqu'à ce qu'une réputation soit établie. Reconsidérer le défaut une fois une adoption et un historique réels en place.
3. **Racines de scan par défaut** : tout le profil utilisateur (lent) vs dossiers de dev détectés ? Proposition : détecter via présence de `.git` + config.
4. **TUI dès v0.1 ou v0.2** ? Proposition : CLI complète d'abord, TUI minimal en M6.
5. **Go vs Python** (voir §8) — Go confirmé, mais voir §12 pour le temps d'apprentissage à budgeter.
6. **Licence** : MIT (adoption max, réutilisable en fermé) vs Apache-2.0 (brevets) vs GPL (protège des forks fermés, freine les contributions). Proposition : **MIT**.

---

## 14. Plan de lancement (sans méthodes trompeuses)

**Avant le lancement**
- README avec GIF avant/après et **chiffres réels** (jamais inventés).
- 2–3 testeurs externes (camarades Epitech, communautés WSL) pour valider sur des machines différentes.
- **[RÉVISÉ]** Le point de validation M1.5 (§12) sert déjà de premier test de marché léger avant le lancement complet.

**Jour J**
- Release `v0.1.0` + article « Pourquoi votre disque WSL ne rétrécit jamais (et comment le récupérer) » (dev.to / blog).
- Post **Show HN** sobre et factuel ; posts dans les communautés pertinentes en respectant leurs règles (WSL, Docker, outils dev, Windows dev).
- Répondre aux threads existants (Microsoft Q&A, forums Docker, issues WSL) **en disant clairement que c'est son propre outil**, uniquement quand c'est utile à la question posée.

**Après**
- Soumission aux listes « awesome » pertinentes, winget, scoop.
- Répondre vite aux issues ; release mineure toutes les 2 semaines au début.

**Interdit** : étoiles achetées, échanges d'étoiles, spam, faux témoignages.

---

## 15. Objectifs mesurables (non garantis)

| Horizon | Signaux | Fourchette indicative [H] |
|---|---|---|
| 30 jours | v0.1 publiée ; retours de testeurs ; premières issues | 20–150★ ; ~10 retours ; 1 contributeur externe |
| 60 jours | issues/PR d'inconnus ; Docker supporté | 100–300★ ; 3–5 issues pertinentes ; 1 PR externe |
| 90 jours | usage récurrent, mentions externes | 200–500★ si le lancement prend ; 2+ contributeurs |

Contexte : dans cette niche, un script de compaction WSL a 112★ [F] ; viser plus suppose un vrai lancement. **[RÉVISÉ]** Le nombre d'étoiles reste un indicateur secondaire — le signal qui compte est celui du point de validation M1.5 : des inconnus utilisent-ils l'outil et le disent-ils, avant même la release complète ?

**Décision continuer / pivoter / abandonner**
- **Continuer** : des inconnus ouvrent des issues ; des utilisateurs citent des Go réellement libérés.
- **Pivoter** (module WSL/Docker seul) : l'intérêt se concentre sur ces parties.
- **Abandonner/réduire** : < 30★ et aucun retour réel à 90 jours malgré une diffusion sérieuse — ou, plus tôt, un signal nul au point de validation M1.5.

---

## 16. Risques et parades **[RÉVISÉ]**

| Risque | Parade |
|---|---|
| Perte de données | garde-fous §6, dry-run, journal, tests bloquants, SECURITY.md, `--trash` par défaut |
| **[RÉVISÉ] Faux positifs antivirus** — proche du résultat par défaut pour un `.exe` neuf non signé qui supprime et touche le registre/WSL, pas un risque marginal | winget/scoop priorisés dès le lancement (pas « juste après »), signature de code budgetée dès M0/M7, SHA256 + attestation, jamais suggérer d'exclusion Defender, build reproductible |
| **[RÉVISÉ] Échec silencieux de `fstrim`** lors de la compaction WSL — la fonctionnalité vitrine du projet peut sembler « cassée » si le gain mesuré est proche de zéro sans explication | mesure avant/après systématique + message explicite si le gain est faible (§5.3, M5) |
| Sparse VHD WSL devient fiable | l'outil reste utile (rapport unifié, caches, artefacts) ; ne pas dépendre de la compaction seule |
| Concurrence (Mole, kondo…) | positionnement : Windows-first, open source, WSL/Docker, estimation du gain réel |
| Admin / Hyper-V absent | repli `diskpart`, messages clairs, code de sortie `3` |
| Surcoût de maintenance (chemins qui changent) | scanners isolés, tests sur sorties enregistrées, issues « nouveau scanner » ouvertes aux contributeurs |
| **[RÉVISÉ] Investissement dans M2–M7 avant tout signal externe** | point de validation M1.5 : diffuser le scanner WSL seul avant de construire le reste |

---

## 17. Checklist de démarrage (jour 1)

- [ ] Vérifier le nom (GitHub, winget, scoop, domaine).
- [ ] Créer le dépôt public, licence MIT, README minimal.
- [ ] `go mod init`, cobra, `devreclaim version`.
- [ ] CI GitHub Actions (Windows) verte.
- [ ] Ouvrir une VM/PC de test avec WSL2 + Docker Desktop.
- [ ] Écrire `docs/what-gets-deleted.md` (v0) **avant** d'écrire le code de suppression.
- [ ] Commencer M1 : `devreclaim wsl status` en lecture seule.
- [ ] **[RÉVISÉ]** Prévoir dès maintenant la diffusion du point de validation M1.5 (à qui le montrer, où le poster) pour ne pas le découvrir après coup.

---

## 18. Sources

- npkill — https://github.com/voidcosmos/npkill
- kondo — https://github.com/tbillington/kondo
- Mole — https://github.com/tw93/mole · https://github.com/tw93/Mole-App · https://mole.fit/windows
- Topic disk-cleanup — https://github.com/topics/disk-cleanup
- microsoft/WSL#12103 — https://github.com/microsoft/WSL/issues/12103
- WSL 3.0.2, sparse VHD — https://xenospectrum.com/en/wsl302-vnuma-sparse-vhd-scope/
- Microsoft Q&A, WSL disk — https://learn.microsoft.com/en-us/answers/questions/4006845/reduce-disk-space-consumption-for-wsl
- freeCodeCamp, WSL disk — https://www.freecodecamp.org/news/how-to-free-up-and-automatically-manage-disk-space-for-wsl-on-windows-1011/
- WSL-VHDX-Compact — https://github.com/hyperphantasia/WSL-VHDX-Compact
- Forum Docker, VHDX — https://forums.docker.com/t/docker-desktop-wsl2-vhdx-grows-by-1-5gb-after-every-update-no-way-to-devreclaim-space/151729
- WSL UI (Octasoft) — https://wsl-ui.octasoft.co.uk/blog/managing-wsl-disk-space
