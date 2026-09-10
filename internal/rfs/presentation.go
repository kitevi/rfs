package rfs

// ItemPresenter supplies source-specific date presentation.
type ItemPresenter interface{ PresentItem(Item) Item }
