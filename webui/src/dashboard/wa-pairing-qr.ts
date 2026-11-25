import { prepareZXingModule, readBarcodes } from 'zxing-wasm/reader';
import wasmURL from 'zxing-wasm/reader/zxing_reader.wasm?url';

prepareZXingModule({ overrides: { locateFile: (path, prefix) => path.endsWith('.wasm') ? wasmURL : prefix + path } });

export async function readPairingQRCode(file: File) {
  if (file.size > 10 * 1024 * 1024 || !['image/png', 'image/jpeg', 'image/webp'].includes(file.type)) {
    throw new Error('请选择 10 MB 以内的 PNG、JPEG 或 WebP 二维码截图');
  }
  const results = await readBarcodes(file, { formats: ['QRCode'], tryHarder: true, maxNumberOfSymbols: 2 });
  if (results.length !== 1 || !results[0].isValid || !results[0].text) throw new Error('请上传只包含一个清晰二维码的截图');
  return results[0].text;
}
