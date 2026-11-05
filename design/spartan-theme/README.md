# Ivaldi theme for Spartan UI

The web app uses [Spartan UI](https://spartan.ng) components. This folder holds Ivaldi's theme for them:

- `ivaldi.css` holds the CSS variables Spartan components read, with Paper (light) under `:root` / `.light` and Forge (dark) under `.dark`. It also has a Tailwind v4 `@theme inline` block for Ivaldi's extra tokens.
- `tokens.json` holds the same values as data, for tools and design files. It mirrors `ivaldi.css`, so change both together.

The design canvas "Ivaldi Web v1" shows every variable and component in both modes on its Theme page, and the sign-in, onboarding and delivery screens built with it.

## How it was derived

The variable set and its modes come from the Spartan UI Figma kit ("Spartan UI for Figma: a 1:1 design kit of Spartan NG"):

| Kit collection | Ivaldi choice |
|---|---|
| Theme (Light, Dark) | Every variable in the collection (`background` … `sidebar-ring`, `chart-1` … `chart-5`) mapped to the brand's five colours. Forge is the default on screen. |
| Radius | `None (0)`: `--radius: 0rem`. |
| Style (Nova, Vega, Maia, Lyra, Mira, Luma) | Lyra geometry (radius 0 for buttons, inputs, cards, badges, tabs and checkboxes) with Vega sizes: controls 24 / 32 / 36 / 40px, 14px text, 24px card padding. |
| Typography | Inter replaces Geist as `--font-sans`. |
| Effects (shadows, elevation) | Not used. Separate surfaces by colour step or border. |

## Using it

Import it after Tailwind and Spartan's preset, then put `class="dark"` on `<html>` for Forge:

```css
@import "tailwindcss";
@import "@spartan-ng/brain/hlm-tailwind-preset.css";
@import "./ivaldi.css";
```

When generating components with the Spartan CLI, pick the Lyra style if it is offered; otherwise the `--radius` and `@theme` radius overrides here square them off.

## Rules the variables can't enforce

- **Ember is a fill, never text on Forge** (2.9:1). Use `bg-destructive text-destructive-foreground` for destructive buttons, badges and alerts, instead of Spartan's subtle `bg-destructive/10 text-destructive` treatment.
- **Bronze text** is `text-primary-text`: bronze on Forge, charcoal on Paper, where bronze fails below 24px. Eyebrow labels use `text-eyebrow`.
- **Headings** use `text-heading` (white on Forge, charcoal on Paper).
- **Never put bronze on steel** (2.7:1). Primary buttons sit on `background` or `card`, not on `muted` or `secondary` panels.
- **Focus** is a 2px `ring` outline with a 2px offset on every interactive element.
- **No shadows or gradients.** Drop `shadow-*` from generated components.
