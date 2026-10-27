# 主题维护

默认主题是 `light.css`，通过 `src/main.tsx` 在组件样式 `style.css`、`styles.css` 之前导入。页面使用白色主题，不随操作系统自动切换深色。

- 配色：修改 `--color-*`。区分页面、卡片、输入框、文字、强调色、状态色和媒体叠层。
- 圆角：修改 `--radius-*`。
- 阴影与焦点：修改 `--shadow-*` 和 `--focus-ring`。
- 字体：修改 `--font-family`、`--font-size-base`。
- `style.css` 和分镜组件样式 `styles.css` 仅维护组件结构、尺寸、间距和响应式布局，不直接写品牌颜色或阴影值。

新增主题时复制主题文件并保留相同变量名称，在 `main.tsx` 中替换导入即可；若以后需要运行时切换，可将自定义变量放在 `:root[data-theme="主题名"]` 下，再由根节点选择。当前不引入主题切换状态或额外依赖。

布局参考 [BeatDesign](https://github.com/BeatAPI/BeatDesign) 的素材与制作工作区分工，以及 [Open Canvas](https://github.com/ZeroLu/open-canvas) 的独立供应商设置。当前实现未复制第三方源码或素材。
