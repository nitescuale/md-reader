---
title: Démo MdReader
tags: [markdown, test]
---

# Démo MdReader

Document de test : ouvre-le avec **MdReader** pour vérifier le rendu de chaque élément.

## Titres et texte

### Niveau 3

#### Niveau 4

##### Niveau 5

###### Niveau 6

Un paragraphe normal, avec du **gras**, de l'*italique*, du ***gras italique***,
du ~~texte barré~~, du `code en ligne`, et une ligne avec deux espaces à la fin
pour forcer un retour à la ligne.

Les entités passent aussi : &amp; &lt; &gt; et les accents éàçùôï, plus un emoji 🚀.

## Listes

- premier point
- deuxième point
  - sous-point
  - sous-point avec `code`
    - encore un niveau
- troisième point

1. étape une
2. étape deux
   1. sous-étape
   2. sous-étape
3. étape trois

- [x] chose faite
- [ ] chose à faire

## Citation

> Une citation sur une ligne,
> qui continue ici.
>
> > Et une citation imbriquée.

## Code

```go
func main() {
	fmt.Println("bloc de code avec indentation préservée")
}
```

```text
Sortie attendue :
  ligne 1
  ligne 2
```

Et un bloc indenté :

    indented code block
    seconde ligne

## Tableau

| Élément | État | Note |
|:---|:---:|---:|
| Titres | oui | 1 à 6 |
| Tableaux | oui | alignement |
| Images | non | lien seulement |
| Recherche | non | à venir |

## Liens

- [lien externe](https://example.com)
- [lien vers un fichier local](README.md) → s'ouvre dans MdReader
- autolien : <https://go.dev>
- URL nue : https://www.markdownguide.org/

## Règle

---

## Image

![logo](assets/icon.ico) → affichée sous forme de lien cliquable.
