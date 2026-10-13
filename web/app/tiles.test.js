import test from 'node:test';
import assert from 'node:assert/strict';

import { parseTiles, tileName, isRed } from './tiles.js';

test('parseTiles 拆分组，组序无关、组内顺序保留', () => {
  const cases = [
    { in: '123m456s789p11z', want: ['1m', '2m', '3m', '4s', '5s', '6s', '7p', '8p', '9p', '1z', '1z'] },
    { in: '456s123m', want: ['4s', '5s', '6s', '1m', '2m', '3m'] },
    { in: '5p', want: ['5p'] },
    { in: '055m', want: ['0m', '5m', '5m'] },
    { in: '111z', want: ['1z', '1z', '1z'] },
    { in: '123m 456p', want: ['1m', '2m', '3m', '4p', '5p', '6p'] },
    { in: '', want: [] },
    { in: null, want: [] },
    { in: '12', want: [] }, // 结尾没有花色字母：坏输入，丢掉
  ];
  for (const c of cases) {
    assert.deepEqual(parseTiles(c.in), c.want, `parseTiles(${JSON.stringify(c.in)})`);
  }
});

test('tileName 覆盖四个花色段、字牌两段循环与赤五', () => {
  const cases = [
    { in: '1m', want: 'Man1' },
    { in: '9m', want: 'Man9' },
    { in: '1s', want: 'Sou1' },
    { in: '9p', want: 'Pin9' },
    { in: '0m', want: 'Man5-Dora' }, // 赤五有单独的牌面，不能并到 Man5
    { in: '0s', want: 'Sou5-Dora' },
    { in: '0p', want: 'Pin5-Dora' },
    { in: '1z', want: 'Ton' },
    { in: '4z', want: 'Pei' },
    { in: '5z', want: 'Haku' },
    { in: '7z', want: 'Chun' },
    // 风牌 4 张、三元牌 3 张是两段独立循环（附录 A 的坑）：0z / 8z 都不该绕回去
    { in: '0z', want: 'Blank' },
    { in: '8z', want: 'Blank' },
    { in: '0x', want: 'Blank' },
    { in: '10m', want: 'Blank' },
    { in: '', want: 'Blank' },
    { in: null, want: 'Blank' },
  ];
  for (const c of cases) {
    assert.equal(tileName(c.in), c.want, `tileName(${JSON.stringify(c.in)})`);
  }
});

test('isRed 只认三张赤五', () => {
  for (const red of ['0m', '0s', '0p']) assert.equal(isRed(red), true, red);
  for (const plain of ['5m', '5s', '5p', '0z', '1m', '', null]) assert.equal(isRed(plain), false, String(plain));
});
