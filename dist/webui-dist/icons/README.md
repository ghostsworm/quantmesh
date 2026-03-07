# PWA 图标生成指南

## 图标要求

PWA 需要多种尺寸的图标以适配不同设备：

- 72x72 - 小图标
- 96x96 - 中图标
- 128x128 - 中图标
- 144x144 - 中图标
- 152x152 - iPad
- 192x192 - Android Chrome
- 384x384 - 大图标
- 512x512 - 启动画面

## 生成方法

### 方法 1: 使用在线工具

1. 访问 https://realfavicongenerator.net/
2. 上传 `icon.svg` 或设计好的 PNG 图标
3. 下载生成的所有尺寸图标
4. 将文件放到此目录

### 方法 2: 使用 ImageMagick (命令行)

```bash
# 安装 ImageMagick
brew install imagemagick  # macOS
# 或
sudo apt-get install imagemagick  # Linux

# 从 SVG 生成不同尺寸的 PNG
for size in 72 96 128 144 152 192 384 512; do
  convert icon.svg -resize ${size}x${size} icon-${size}x${size}.png
done
```

### 方法 3: 使用 Node.js 脚本

```bash
# 安装依赖
npm install sharp

# 运行生成脚本
node generate-icons.js
```

## 当前状态

目前使用占位符 SVG 图标。在生产环境部署前，请：

1. 设计专业的应用图标
2. 使用上述方法生成所有尺寸
3. 替换此目录下的文件

## 设计建议

- 使用简洁的图标设计
- 确保在小尺寸下仍然清晰可辨
- 使用品牌颜色（主色：#3182ce）
- 考虑深色和浅色背景的兼容性
- 为 maskable 图标预留安全区域（20% 边距）

