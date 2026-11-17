// The landing page's cards (overrides/landing.html): each card shows one
// part of a page the docs already capture whole, so its capture is that
// capture's recipe again, with the same fixture, driving and checks, taken
// as a clip of the region the card shows rather than as the whole page. The
// card then loads a picture of that region alone instead of a full-page
// capture scaled and cropped in the browser.
//
// A clip is in the source page's CSS pixels, from the document's top-left
// (run.mjs takes it with `fullPage`, so it may lie below the fold), at the
// source recipe's own device scale factor. Keep its shape that of the card's
// frame on a wide screen (landing.html's `.frame` heights at 1440 wide), so
// the card shows all of it there.
//
// cardRecipes(recipes, [[source name, card name, clip], ...]) -> the card
// recipes, to append to the file's own list.
export function cardRecipes(recipes, cards) {
  return cards.map(([source, name, clip]) => {
    const recipe = recipes.find((r) => r.name === source);
    if (!recipe) throw new Error(`card ${name}: no recipe named ${source}`);
    return { ...recipe, name, capture: { clip } };
  });
}
