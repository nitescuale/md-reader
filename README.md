# MdReader

Liseuse Markdown portable pour Windows : un seul `.exe` (≈2,3 Mo), aucune dépendance, aucun installeur.
Double-clic sur un `.md`, ou glisser-déposer sur la fenêtre.

Écrite en Go, elle affiche le markdown via le contrôle **RichEdit** de Windows (`Msftedit.dll`) :
le markdown est converti en RTF par un package interne (`md/`), donc pas de moteur de navigateur,
pas d'Electron, pas de 200 Mo de `node_modules`.

## Fonctionnalités

| | |
|---|---|
| Rendu | titres, listes (imbriquées), cases à cocher, tableaux GFM, citations, blocs de code, règles, `**gras**`, `*italique*`, `~~barré~~`, `` `code` `` |
| Liens | cliquables ; un lien vers un autre `.md` s'ouvre **dans l'app**, le reste part au navigateur / à l'app par défaut |
| Édition | `Ctrl+E` bascule lecture ↔ édition du markdown brut (discret : l'indicateur est dans la barre d'état) |
| Enregistrement | `Ctrl+S` en mode édition, avec confirmation à la fermeture si le document est modifié |
| Suivi disque | le fichier est rechargé automatiquement s'il change sur le disque (sauf modifications non enregistrées) |
| Confort | thème clair/sombre/auto, zoom, plein écran, fichiers récents, drag & drop, mémorisation de la position de fenêtre |
| Portable | les réglages sont écrits dans `mdreader.json` **à côté de l'exe** (repli sur `%APPDATA%\MdReader` si le dossier n'est pas inscriptible) |

## Raccourcis

| Raccourci | Action |
|---|---|
| `Ctrl+O` | ouvrir |
| `Ctrl+E` | lecture / édition |
| `Ctrl+S` | enregistrer (mode édition) |
| `F5` | recharger depuis le disque |
| `Ctrl` + molette, `Ctrl+0` | zoom |
| `F11` / `Échap` | plein écran |
| `Ctrl+L` / `Ctrl+D` | thème clair / sombre |

## En faire l'application par défaut pour les `.md`

Menu **Fichier → Définir comme lecteur .md par défaut**. L'association est écrite dans
`HKCU\Software\Classes` (aucun droit administrateur, aucune modification globale de la machine),
pour `.md`, `.markdown`, `.mdown` et `.mkd`.

Windows 10/11 protège le choix de l'utilisateur : si une autre application avait déjà été choisie
explicitement, l'entrée existante n'est pas écrasée et il faut confirmer une fois via
**Fichier → Choisir via « Ouvrir avec »…**, puis *Toujours utiliser cette application*.
L'entrée inverse existe aussi : **Retirer l'association .md**.

## Construire

Prérequis : Go 1.21+ (aucun compilateur C, `CGO_ENABLED=0`).

```bash
# depuis Linux/macOS (cross-compilation) ou depuis Windows
./build.sh          # -> dist/MdReader.exe
build.bat           # équivalent Windows
```

Régénérer l'icône et les ressources Windows (facultatif, nécessite python3 et `go-winres`) :

```bash
python3 tools/genicon.py assets/icon.ico     # icône multi-tailles, sans dépendance
go install github.com/tc-hib/go-winres@latest
go-winres make --in winres/winres.json --arch amd64 --out rsrc
```

## Structure

```
main.go      fenêtre, menus, raccourcis, lecture/écriture de fichier, bascule lecture/édition
win32.go     liaisons Win32 minimales (user32, msftedit, comdlg32, advapi32…)
assoc.go     réglages persistants, détection du thème système, association .md
md/          conversion markdown -> RTF + texte miroir + table des liens (testé unitairement)
tools/       générateur d'icône (pur Python)
winres/      icône, version, manifeste (contrôles v6, DPI per-monitor)
```

Le package `md` est indépendant de Windows et couvert par `go test ./md` :
échappement RTF, équilibrage des accolades, contenu du texte miroir, positions des liens,
tableaux, listes imbriquées, cas limites (entrées malformées, emoji, CRLF…).

## Limites connues

- Les images sont affichées sous forme de lien cliquable (pas d'affichage inline) — RichEdit n'est pas un moteur de rendu HTML.
- Pas encore de recherche dans le document (`Ctrl+F`), ni d'impression.
- HTML brut dans le markdown : les balises sont retirées, pas interprétées.
- Windows 10 minimum (manifeste per-monitor v2, contrôles v6).
