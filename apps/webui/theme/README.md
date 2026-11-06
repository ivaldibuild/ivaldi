# Ivaldi theme for Spartan UI

The web UI's components come from [Spartan UI](https://spartan.ng) (`libs/ui`). Their colours, corners and fonts come from the Ivaldi theme in `src/styles.css`:

- `:root` is **Paper** (light), for documents and anyone who prefers it.
- `:root.dark` is **Forge** (dark), the default. `src/index.html` sets `class="dark"` on `<html>`.
- `tokens.json` in this folder holds the same values as data, for tools and design files. `src/styles.css` is the source of truth, so change both together.

The design canvas "Ivaldi Web v1" shows every variable and component in both modes on its Theme page, and the sign-in, onboarding and delivery screens built with them.

## How it was derived

The variable set and its modes come from the Spartan UI Figma kit ("Spartan UI for Figma: a 1:1 design kit of Spartan NG"):

| Kit collection                             | Ivaldi choice                                                                                                                                                                  |
| ------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Theme (Light, Dark)                        | Every variable (`background` … `sidebar-ring`, `chart-1` … `chart-5`) mapped to the brand's five colours: charcoal, steel, bronze, stone and ember, plus white and warm white. |
| Radius                                     | `None (0)`: `--radius: 0rem`. Spartan's preset derives `rounded-sm` … `rounded-4xl` from it, so they are all square.                                                           |
| Style (Nova, Vega, Maia, Lyra, Mira, Luma) | Lyra's square geometry with Vega's sizes. `components.json` stays on `vega`.                                                                                                   |
| Typography                                 | Inter (bundled with `@fontsource-variable/inter`, so a self-hosted install makes no font requests) replaces Geist as `--font-sans`.                                            |
| Effects                                    | The `shadow-*` utilities draw nothing. Separate surfaces by colour step or border.                                                                                             |

## Extra tokens

Spartan's variables can't express everything in the brand rules, so the theme adds these, each with a Tailwind utility:

| Variable                   | Utility                       | Forge        | Paper        | Use                                          |
| -------------------------- | ----------------------------- | ------------ | ------------ | -------------------------------------------- |
| `--heading`                | `text-heading`                | white        | charcoal     | Headings and the display line                |
| `--eyebrow`                | `text-eyebrow`                | bronze       | steel        | Labels set in capitals with wide tracking    |
| `--primary-text`           | `text-primary-text`           | bronze       | charcoal     | Bronze as text; it fails on Paper below 24px |
| `--destructive-foreground` | `text-destructive-foreground` | white        | white        | Text on an ember fill                        |
| `--destructive-text`       | `text-destructive-text`       | white        | ember        | Error text: ember fails as text on Forge     |
| `--overlay`                | `bg-overlay`                  | charcoal 80% | charcoal 60% | Dialog and sheet backdrops                   |

## How `libs/ui` follows the brand

The generated components have been adjusted where a theme variable alone could not make them follow the brand:

- **Destructive fills, not tints.** The destructive button, badge, alert and chat bubble, a destructive dropdown item on hover or focus, and a failed attachment's thumbnail use `bg-destructive text-destructive-foreground`. Spartan's `bg-destructive/10` tints are gone, except behind removed lines in a code diff.
- **Error text uses `text-destructive-text`**: field and questionnaire errors, invalid labels and radios, failed trace lines, destructive code blocks and dropdown items. It is ember on Paper and white on Forge.
- **Invalid controls keep a solid ember border in both modes.** The `dark:…:border-destructive/50` overrides are removed.
- **Square corners.** Switches, progress bars, sliders, carousel buttons, the drawer handle, bubble reactions, avatar badges and calendar markers no longer use `rounded-full`. Avatars use the app-icon radius (`rounded-[19.5%]`). Radio buttons stay round, so they can't be mistaken for checkboxes.

Regenerating a component with the Spartan CLI brings back its default classes, so re-apply these changes after an update.

## Rules the variables can't enforce

- **Never put bronze on steel** (2.7:1). Primary buttons sit on `background` or `card`, not on `muted` or `secondary` panels.
- **Ember appears as a fill or a border**, with a word or icon so the state never relies on colour. For error text use `text-destructive-text`, never `text-destructive`.
- **Focus** is a visible `ring` outline on every interactive element.
- **No gradients.**
