// Package position moves a place in one commit to the place it has become in another. That is
// how a comment written against an old snapshot is drawn against a newer one.
//
// A review comment is written against a round, which is a frozen commit of an agent's worktree,
// and the agent keeps writing afterwards. To draw that comment against a later round something
// has to say where those lines went. Both rounds are ordinary commits weave still holds, so the
// answer is in git rather than in a guess, and this package is the arithmetic that reads it.
//
// A range holds its start and not its end. That choice does two things. It makes a span of no
// lines a point between two lines, which is what an insertion is, and it lets two regions meet
// end to start without overlapping. Lines are numbered from one. There are no character offsets
// anywhere, because git only works to line precision.
//
// Paths are resolved before any line is counted, and the order is load-bearing. A rename or a
// copy is settled first, so the line walk never has to know a file moved, and a file copied to
// two paths turns one position into two results rather than forcing a choice between them.
//
// The line walk carries a running shift down the file. A region above a position moves it. A
// region the position overlaps is a conflict. Lines arriving inside a position make it longer
// rather than moving where it starts, because a comment on a block still covers that block when
// something is added to the middle of it.
//
// What to do with a conflict is the caller's to say, not this package's, which is why Policy is
// an argument. A comment wants to survive as something said about the file, so it keeps the file
// and loses the lines; a fold or a mark wants to disappear rather than mislead.
//
// The design is Gerrit's GitPositionTransformer, which has done this since 2020 and had the
// sense to keep the transformer ignorant of comments. Reimplemented here rather than translated,
// and the same ignorance is the reason this package imports nothing of weave's.
package position
