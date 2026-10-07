// lineup_save.jsx: what a lineup save carries, and how two saves of one lineup join in
// the offline queue (operator decision 2026-10-07, "Only changed positions").
//
// A save names the positions it changed: { positions, memberIds?, changed }. The
// server reads only those positions and lands them on the lineup it holds when the
// save arrives, so two devices changing different positions of one lineup both keep
// their change, and a save made with no connection says no more than one made with
// it. A body with no `changed` is a whole lineup and replaces the stored one: what a
// save queued by an earlier build replays as.
//
// A leaf with no imports, ES-imported by api_client.jsx (the queue) and the editors
// (the body); there is no window mirror.

// changedLineupSave: the body of a save of the positions `changed`, read off the
// lineup as the form shows it. A name is there for every changed position, "" for
// one the operator cleared (the server takes a changed position with no name for an
// error), and memberIds holds the ids of the positions that have one: a name "" with
// an id is a member picked before it was named, a placement of its own.
export function changedLineupSave(positions, memberIds, changed) {
  const body = { positions: {}, memberIds: {}, changed: [...changed] };
  changed.forEach((key) => {
    body.positions[key] = positions[key] || '';
    if (memberIds[key]) body.memberIds[key] = memberIds[key];
  });
  return body;
}

// joinQueuedLineupSave: the save that takes the place of a queued save of the same
// lineup (`queued`) when `incoming` is made, so the lineup has ONE entry in the
// queue. It changes every position either changed, and where both did the later
// save's value stands, name and id (a position it cleared is "" with no id).
//
// A queued save that names no changed positions is a whole lineup, queued by an
// earlier build, and stays whole: the later save's changes are applied onto that
// lineup (a position it cleared is left out, as such a build sent it), and the join
// names none either. A later save that names none is a whole lineup itself and takes
// the queued one's place as it is.
//
// A copy, never a mutation: the request in flight and the broadcast share the objects.
export function joinQueuedLineupSave(queued, incoming) {
  if (!Array.isArray(incoming.changed)) return incoming;
  const whole = !Array.isArray(queued.changed);
  const positions = { ...queued.positions };
  const memberIds = { ...queued.memberIds };
  incoming.changed.forEach((key) => {
    const name = incoming.positions[key];
    const id = (incoming.memberIds && incoming.memberIds[key]) || '';
    if (whole && !name && !id) delete positions[key];
    else positions[key] = name;
    if (id) memberIds[key] = id;
    else delete memberIds[key];
  });
  const joined = { ...incoming, positions };
  if (queued.memberIds || incoming.memberIds) joined.memberIds = memberIds;
  if (whole) delete joined.changed;
  else joined.changed = [...queued.changed, ...incoming.changed.filter((key) => !queued.changed.includes(key))];
  return joined;
}
