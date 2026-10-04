# 牌面资源来源

本目录下的 40 个 `.svg` 全部来自：

- **仓库**：https://github.com/FluffyStuff/riichi-mahjong-tiles
- **钉定 commit**：`26e127ba2117f45cdce5ea0225748cc0cfad3169`
- **路径**：`Regular/`
- **许可证**：CC0 1.0 公共领域（见同目录 `LICENSE.md`）
- **抓取方式**：`cdn.jsdelivr.net/gh/FluffyStuff/riichi-mahjong-tiles@master/Regular/<name>.svg`

CC0 意味着可以商用、修改、不署名。本文件与 `LICENSE.md` 保留仅为了可追溯。

## 文件清单

| 分组 | 文件 |
| --- | --- |
| 万 | `Man1.svg` … `Man9.svg` |
| 筒 | `Pin1.svg` … `Pin9.svg` |
| 索 | `Sou1.svg` … `Sou9.svg` |
| 风牌 | `Ton.svg`(东) `Nan.svg`(南) `Shaa.svg`(西) `Pei.svg`(北) |
| 三元牌 | `Haku.svg`(白) `Hatsu.svg`(发) `Chun.svg`(中) |
| 赤宝牌 | `Man5-Dora.svg` `Pin5-Dora.svg` `Sou5-Dora.svg` |
| 其他 | `Back.svg`(牌背) `Front.svg`(牌面模板) `Blank.svg`(空白牌) |

## 三条使用须知

1. **画布比例 3:4。** 每张 SVG 都是 `width="300" height="400" viewBox="0 0 300 400"`。显示尺寸取 60×80 之类时保持 3:4。
2. **`Haku.svg`（白）是空图层。** 传统牌面上白板就是留白，不是资源缺失。三元牌的中文名（白/发/中）只出现在役种说明等文字里，不画在牌面上。
3. **上游另有 `Black/` 目录**，与 `Regular/` 一一对应，是深色牌面。将来要做深色主题时是现成的一套。

## 未采用的做法（记录以免重复讨论）

- **不做离屏预渲染**：SVG 内禀 300×400、显示 60×80 是**缩小**，浏览器栅格化后已经锐利；`ctx.drawImage(img, …)` 直出即可，不需要启动时预烘成 `ImageBitmap`。
- **不做 SVGO 压缩**：这些文件含大量 Inkscape 编辑器元数据（`sodipodi:namedview`、`inkscape:path-effect`、未使用的 `<defs>`），但资源是 embed 进 Go 二进制本地提供的，网络体积无意义；为此加一条构建工具链不划算。
- **不用 `Path2D` 解析路径**：源文件带 `clipPath`/`linearGradient`/`pattern`/内联 `style`/`transform`，要正确渲染等于自己写一个 SVG 子集渲染器。交给浏览器的 SVG 渲染器。
