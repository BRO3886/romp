# romp website

The Hugo site for romp. Light mode only.

```bash
cd website
hugo server
```

Build the production site with:

```bash
hugo --minify
```

Hugo writes the generated output to `website/public/`. Do not commit that
directory.

The theme lives in `themes/romp-docs/`. Content lives in `content/`. The logo
and mascot are derived from the original artwork under `static/`.
