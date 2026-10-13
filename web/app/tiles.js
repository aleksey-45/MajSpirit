// 牌的紧凑记法与资源名互转（协议 §2.2）。
//
// 只做「记法 → 牌码 → SVG 文件名」这一条链。和牌判定、役种、符数一律不在这里：
// 客户端不做任何规则判定（协议 §9.3）。

const HONORS = ['Ton', 'Nan', 'Shaa', 'Pei', 'Haku', 'Hatsu', 'Chun'];
const SUITS = { m: 'Man', s: 'Sou', p: 'Pin' };

// BLANK 是认不出来的牌画出来的东西。
//
// 不抛错是有意的：一张坏牌不该把整张桌子打成白屏 —— 调试客户端的第一职责是
// 「把服务端发的东西显示出来」，显示成一个空白牌比什么都不显示有用。
const BLANK = 'Blank';

// parseTiles 把 "123m456s11z" 拆成 ['1m','2m',...]。
//
// 组序无意义、组内顺序有意义（协议 §2.2），所以只管拆、不排序：
// 排掉顺序就丢了服务端给的物理排列。
export function parseTiles(text) {
  const out = [];
  let digits = '';
  for (const ch of String(text ?? '')) {
    if (ch >= '0' && ch <= '9') {
      digits += ch;
      continue;
    }
    if (SUITS[ch] || ch === 'z') {
      for (const d of digits) out.push(d + ch);
      digits = '';
    }
    // 其余字符（空白、逗号）只当分隔符，不进结果。
  }
  // 结尾挂着的数字丢掉：那是坏输入，不是牌。
  return out;
}

// tileName 把牌码映射到 web/public/tiles/ 下的文件名。
//
// 赤五和普通五是同一「种类」、不同编号（协议 §2.2），所以 0m/0s/0p 各有一张
// 单独的牌面，不能并到 5m/5s/5p。
export function tileName(code) {
  if (typeof code !== 'string' || code.length !== 2) return BLANK;
  const digit = code[0];
  const suit = code[1];
  if (suit === 'z') {
    const n = Number(digit);
    return n >= 1 && n <= 7 ? HONORS[n - 1] : BLANK; // 字牌没有赤字
  }
  const base = SUITS[suit];
  if (!base) return BLANK;
  const n = Number(digit);
  if (n === 0) return base + '5-Dora';
  return n >= 1 && n <= 9 ? base + n : BLANK;
}

// isRed 判断是不是赤五。牌桌用它给赤五换红色边框。
export function isRed(code) {
  return typeof code === 'string' && code.length === 2 && code[0] === '0' && SUITS[code[1]] !== undefined;
}
