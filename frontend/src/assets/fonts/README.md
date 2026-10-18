# Local fonts

The application bundles its fonts so rendering does not depend on fonts
installed on the user's machine.

| Local family | Source | Bundled weights |
| --- | --- | --- |
| Neutron Sans | Noto Sans SC v41 | 400, 500, 600 |
| Neutron Mono | Maple Mono CN v7.9 | 400 |

The Noto Sans SC files are subset to Latin, punctuation, symbols, and common
CJK ranges, then converted to WOFF2. The original font files are available
from Google Fonts. Maple Mono CN is sourced from
`https://github.com/subframe7536/maple-font/releases/tag/v7.9`.

All bundled fonts use the SIL Open Font License. Their license texts are kept
next to the font files.
