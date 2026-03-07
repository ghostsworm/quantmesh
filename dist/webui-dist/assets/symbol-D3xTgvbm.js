const s=["USDT","USDC","BUSD","FDUSD","DAI","U"];function S(e){if(!e)return"USDT";const n=e.toUpperCase(),o=[...s].sort((t,r)=>r.length-t.length);for(const t of o)if(n.endsWith(t)&&n.length>t.length)return t;return"USDT"}export{S as g};
//# sourceMappingURL=symbol-D3xTgvbm.js.map
