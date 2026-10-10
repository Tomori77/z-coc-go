import { cp, copyFile, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const projectRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const webRoot = path.join(projectRoot, 'web');
const outputRoot = path.join(projectRoot, 'www');

if (path.dirname(outputRoot) !== projectRoot || path.basename(outputRoot) !== 'www') {
    throw new Error(`Refusing to clear unexpected mobile output path: ${outputRoot}`);
}

await rm(outputRoot, { recursive: true, force: true });
await mkdir(outputRoot, { recursive: true });

for (const directory of ['assets', 'Library', 'Workshop']) {
    await cp(path.join(webRoot, directory), path.join(outputRoot, directory), { recursive: true });
}

for (const file of ['index.html', '404.html', 'favicon.svg', 'manifest.json']) {
    await copyFile(path.join(webRoot, file), path.join(outputRoot, file));
}

// The default BGM is stored under an ASCII name because go:embed rejects the
// original Unicode-punctuated name; restore the runtime URL for the mobile bundle.
await copyFile(
    path.join(outputRoot, 'assets', 'audio', 'rainy-alley-night.mp3'),
    path.join(outputRoot, 'assets', 'audio', '雨夜（街巷）.mp3'),
);

const vendorRoot = path.join(outputRoot, 'assets', 'vendor');
await mkdir(vendorRoot, { recursive: true });

const vendorFiles = new Map([
    ['node_modules/vue/dist/vue.global.prod.js', 'vue.global.prod.js'],
    ['node_modules/marked/lib/marked.umd.js', 'marked.umd.js'],
    ['node_modules/dompurify/dist/purify.min.js', 'purify.min.js'],
    ['node_modules/mammoth/mammoth.browser.min.js', 'mammoth.browser.min.js'],
    ['node_modules/file-saver/dist/FileSaver.min.js', 'FileSaver.min.js'],
    ['node_modules/html-docx-js/dist/html-docx.js', 'html-docx.js'],
    ['node_modules/localforage/dist/localforage.min.js', 'localforage.min.js'],
    ['node_modules/daisyui/dist/full.css', 'daisyui.full.css'],
]);

for (const [source, target] of vendorFiles) {
    await copyFile(path.join(projectRoot, source), path.join(vendorRoot, target));
}

const replacements = new Map([
    ['https://cdn.jsdelivr.net/npm/vue@3.5.13/dist/vue.global.prod.js', 'vue.global.prod.js'],
    ['https://cdn.jsdelivr.net/npm/marked@12.0.2/marked.min.js', 'marked.umd.js'],
    ['https://cdn.jsdelivr.net/npm/dompurify@3.0.6/dist/purify.min.js', 'purify.min.js'],
    ['https://cdnjs.cloudflare.com/ajax/libs/mammoth/1.6.0/mammoth.browser.min.js', 'mammoth.browser.min.js'],
    ['https://cdnjs.cloudflare.com/ajax/libs/FileSaver.js/2.0.5/FileSaver.min.js', 'FileSaver.min.js'],
    ['https://cdn.jsdelivr.net/npm/html-docx-js@0.3.1/dist/html-docx.js', 'html-docx.js'],
    ['https://cdn.jsdelivr.net/npm/localforage@1.10.0/dist/localforage.min.js', 'localforage.min.js'],
    ['https://cdn.jsdelivr.net/npm/daisyui@4.7.2/dist/full.min.css', 'daisyui.full.css'],
]);

for (const relativeFile of ['index.html', 'Library/index.html', 'Workshop/index.html']) {
    const target = path.join(outputRoot, relativeFile);
    const prefix = relativeFile === 'index.html' ? 'assets/vendor/' : '../assets/vendor/';
    let html = await readFile(target, 'utf8');
    for (const [remoteUrl, localFile] of replacements) {
        html = html.replaceAll(remoteUrl, `${prefix}${localFile}`);
    }
    await writeFile(target, html, 'utf8');
}

console.log(`Mobile web bundle created at ${outputRoot}`);
