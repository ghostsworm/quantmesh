#!/usr/bin/env node

/**
 * PWA 图標生成脚本
 * 
 * 使用方法:
 * 1. 确保已安装 sharp: npm install sharp
 * 2. 准备源图標文件 icon-source.png (建议 1024x1024 或更大)
 * 3. 运行: node generate-icons.js
 */

const sharp = require('sharp');
const fs = require('fs');
const path = require('path');

const sizes = [72, 96, 128, 144, 152, 192, 384, 512];
const sourceFile = path.join(__dirname, 'icon-source.png');

async function generateIcons() {
  // 检查源文件是否存在
  if (!fs.existsSync(sourceFile)) {
    console.error('❌ 錯误: 找不到源图標文件 icon-source.png');
    console.log('💡 请准备一個 1024x1024 或更大的 PNG 图標文件，命名為 icon-source.png');
    process.exit(1);
  }

  console.log('🎨 开始生成 PWA 图標...\n');

  for (const size of sizes) {
    const outputFile = path.join(__dirname, `icon-${size}x${size}.png`);
    
    try {
      await sharp(sourceFile)
        .resize(size, size, {
          fit: 'contain',
          background: { r: 49, g: 130, b: 206, alpha: 1 } // #3182ce
        })
        .png()
        .toFile(outputFile);
      
      console.log(`✅ 生成: icon-${size}x${size}.png`);
    } catch (error) {
      console.error(`❌ 生成 ${size}x${size} 失败:`, error.message);
    }
  }

  console.log('\n🎉 图標生成完成！');
}

generateIcons().catch(console.error);

