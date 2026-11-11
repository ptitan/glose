// Verify the actual grid and painted sidebar, including the return-to-text link.
const assert = require('node:assert/strict');
module.exports = async ({p,page}) => {
 await page.evaluate(()=>sessionStorage.setItem('glose-texte','Titre très long '.repeat(30)));
 for (const route of ['/set','/home']) {
  await page.navigate(new URL(route+'#t='+p.state.token,p.state.url).href, route==='/set'?'#ra-statut':'#acc-lignes');
  for (const width of [1000,840,780,540,1000]) {
   await page.viewport(width,900);
   await page.wait('largeur réelle du navigateur',w=>window.innerWidth===w,width);
   const geometry = await page.evaluate(()=>{
    const nav = document.querySelector('.acc-nav'), rect=nav.getBoundingClientRect();
    return {width:rect.width, track:parseFloat(getComputedStyle(document.body).gridTemplateColumns),
      main:nav.nextElementSibling.getBoundingClientRect().left,
      returnText:getComputedStyle(document.querySelector('.acc-revenir span')).display};
   });
   const expected = width<=800?56:64;
   assert.equal(geometry.width,expected,route+' : largeur de colonne à '+width);
   assert.equal(geometry.track,expected); assert.equal(geometry.main,expected);
   assert.equal(geometry.returnText,'none','texte du retour masqué dans la colonne repliée');
  }
 }
};
