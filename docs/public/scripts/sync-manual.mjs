#!/usr/bin/env node
import { cp, rm, mkdir, readdir, readFile, writeFile } from 'node:fs/promises';
import { join, relative, dirname, basename } from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const rootDir = join(__dirname, '..');
const manualDir = join(rootDir, '../manual');
const contentDir = join(rootDir, 'src/content/docs');

async function ensureDir(dir) {
  try {
    await mkdir(dir, { recursive: true });
  } catch {}
}

function escapeYaml(str) {
  if (!str) return '';
  if (str.includes(':') || str.includes('"') || str.includes("'") || str.includes('\n')) {
    return '"' + str.replace(/"/g, '\\"').replace(/\n/g, ' ').trim() + '"';
  }
  return str;
}

async function processMarkdown(content, filename) {
  const lines = content.split('\n');
  let title = '';
  let description = '';
  
  for (const line of lines) {
    if (line.startsWith('# ') && !title) {
      title = line.slice(2).trim();
    }
    if (title && line.trim() && !line.startsWith('#') && !line.startsWith('Snapshot date') && !description) {
      description = line.trim().slice(0, 160);
      if (description.length === 160) description += '...';
    }
  }
  
  const frontmatter = `---
title: ${escapeYaml(title || filename)}
description: ${escapeYaml(description || title || 'Documentation')}
---

`;
  
  let processed = content;
  if (lines[0]?.startsWith('# ')) {
    processed = lines.slice(1).join('\n').trimStart();
  }
  
  return frontmatter + processed;
}

async function copyAndProcess(srcDir, destDir, basePath = '') {
  await ensureDir(destDir);
  const entries = await readdir(srcDir, { withFileTypes: true });
  
  for (const entry of entries) {
    const srcPath = join(srcDir, entry.name);
    const destPath = join(destDir, entry.name);
    
    if (entry.isDirectory()) {
      if (entry.name !== 'node_modules' && !entry.name.startsWith('.')) {
        await copyAndProcess(srcPath, destPath, join(basePath, entry.name));
      }
    } else if (entry.name.endsWith('.md')) {
      const content = await readFile(srcPath, 'utf-8');
      const processed = await processMarkdown(content, entry.name);
      await writeFile(destPath, processed);
    }
  }
}

async function main() {
  console.log('Syncing manual content...');
  
  await rm(join(contentDir, 'start-here'), { recursive: true, force: true });
  await rm(join(contentDir, 'personas'), { recursive: true, force: true });
  await rm(join(contentDir, 'workflows'), { recursive: true, force: true });
  await rm(join(contentDir, 'admin-extensibility'), { recursive: true, force: true });
  await rm(join(contentDir, 'operations'), { recursive: true, force: true });
  await rm(join(contentDir, 'reference'), { recursive: true, force: true });
  await rm(join(contentDir, 'troubleshooting'), { recursive: true, force: true });
  await rm(join(contentDir, 'appendix'), { recursive: true, force: true });
  
  const startHere = await readFile(join(manualDir, 'start-here.md'), 'utf-8');
  const processedStart = await processMarkdown(startHere, 'start-here.md');
  await ensureDir(contentDir);
  await writeFile(join(contentDir, 'index.mdx'), processedStart);
  
  const subdirs = ['personas', 'workflows', 'admin-extensibility', 'operations', 'reference', 'troubleshooting', 'appendix'];
  
  for (const subdir of subdirs) {
    console.log(`  Copying ${subdir}...`);
    await copyAndProcess(
      join(manualDir, subdir),
      join(contentDir, subdir)
    );
  }
  
  console.log('Done!');
}

main().catch(console.error);
