// Interactive walk-through of the pool draw, for "Who lands in which pool"
// on the pool draw page.
//
// The draw places one competitor at a time by descending a tree built over
// the knockout slot each pool's winner will feed. This widget replays that
// descent: it shows the competitor being placed, the branch chosen at every
// fork and the reason for it, and the pools filling up underneath.
//
// The rules below are the ones the application itself applies
// (internal/helper, chooseDojoTreePool and its neighbours). The example
// rosters and the pools they produce are not hand-written: the fixture
// between the BCDA-FIXTURE markers is valid JSON, and a Go test reads this
// very file and re-runs each roster through the real distributor to confirm
// the recorded pools are the ones the application draws. Change the fixture
// and that test tells you at once whether the page still tells the truth.
//
// Material's navigation.instant swaps pages over XHR, so the widget mounts
// from the document$ observable rather than DOMContentLoaded, exactly as
// mermaid-init.js does.
(function () {
  "use strict";

  /* BCDA-FIXTURE-START */
  var BCDA_FIXTURE = {
    "presets": [
      {
        "id": "eight-pools",
        "label": "Eight pools of three",
        "blurb": "Twenty-four entrants from four dojos, six from each. No dojo has more members than there are pools, so every one of them can be kept apart.",
        "poolSize": 3,
        "poolSizeIsMaximum": true,
        "courts": 1,
        "poolWinners": 1,
        "poolNames": ["Pool A", "Pool B", "Pool C", "Pool D", "Pool E", "Pool F", "Pool G", "Pool H"],
        "poolSizes": [3, 3, 3, 3, 3, 3, 3, 3],
        "winnerSlots": [0, 1, 2, 3, 4, 5, 6, 7],
        "roster": [
          {"name": "Abbott", "dojo": "Ashford"},
          {"name": "Barrow", "dojo": "Coalport"},
          {"name": "Chase", "dojo": "Ashford"},
          {"name": "Dalton", "dojo": "Brookvale"},
          {"name": "Ellis", "dojo": "Deerhurst"},
          {"name": "Fenn", "dojo": "Brookvale"},
          {"name": "Grady", "dojo": "Coalport"},
          {"name": "Hale", "dojo": "Ashford"},
          {"name": "Innes", "dojo": "Deerhurst"},
          {"name": "Judd", "dojo": "Coalport"},
          {"name": "Keane", "dojo": "Brookvale"},
          {"name": "Lowry", "dojo": "Deerhurst"},
          {"name": "Mercer", "dojo": "Ashford"},
          {"name": "Naylor", "dojo": "Deerhurst"},
          {"name": "Oakes", "dojo": "Brookvale"},
          {"name": "Pike", "dojo": "Coalport"},
          {"name": "Quinn", "dojo": "Ashford"},
          {"name": "Rowe", "dojo": "Brookvale"},
          {"name": "Sharpe", "dojo": "Deerhurst"},
          {"name": "Tate", "dojo": "Coalport"},
          {"name": "Usher", "dojo": "Brookvale"},
          {"name": "Vance", "dojo": "Ashford"},
          {"name": "Wren", "dojo": "Coalport"},
          {"name": "Yates", "dojo": "Deerhurst"}
        ],
        "poolsAfterDescent": [
          ["Abbott", "Keane", "Wren"],
          ["Barrow", "Naylor", "Quinn"],
          ["Dalton", "Judd", "Sharpe"],
          ["Ellis", "Mercer", "Usher"],
          ["Chase", "Innes", "Rowe"],
          ["Grady", "Oakes", "Vance"],
          ["Fenn", "Lowry", "Tate"],
          ["Hale", "Pike", "Yates"]
        ],
        "exchanges": [],
        "pools": [
          ["Abbott", "Keane", "Wren"],
          ["Barrow", "Naylor", "Quinn"],
          ["Dalton", "Judd", "Sharpe"],
          ["Ellis", "Mercer", "Usher"],
          ["Chase", "Innes", "Rowe"],
          ["Grady", "Oakes", "Vance"],
          ["Fenn", "Lowry", "Tate"],
          ["Hale", "Pike", "Yates"]
        ]
      },
      {
        "id": "crowded-dojo",
        "label": "Six pools of four",
        "blurb": "The same twenty-four entrants, ten of them from Ashford. Ten members cannot fit into six pools one apiece, so some of them must share, and the draw spreads them two, two, two, two, one and one.",
        "poolSize": 4,
        "poolSizeIsMaximum": true,
        "courts": 1,
        "poolWinners": 1,
        "poolNames": ["Pool A", "Pool B", "Pool C", "Pool D", "Pool E", "Pool F"],
        "poolSizes": [4, 4, 4, 4, 4, 4],
        "winnerSlots": [0, 1, 2, 3, 4, 5],
        "roster": [
          {"name": "Abbott", "dojo": "Ashford"},
          {"name": "Barrow", "dojo": "Brookvale"},
          {"name": "Chase", "dojo": "Ashford"},
          {"name": "Dalton", "dojo": "Coalport"},
          {"name": "Ellis", "dojo": "Ashford"},
          {"name": "Fenn", "dojo": "Deerhurst"},
          {"name": "Grady", "dojo": "Ashford"},
          {"name": "Hale", "dojo": "Eastleigh"},
          {"name": "Innes", "dojo": "Ashford"},
          {"name": "Judd", "dojo": "Brookvale"},
          {"name": "Keane", "dojo": "Ashford"},
          {"name": "Lowry", "dojo": "Coalport"},
          {"name": "Mercer", "dojo": "Ashford"},
          {"name": "Naylor", "dojo": "Deerhurst"},
          {"name": "Oakes", "dojo": "Ashford"},
          {"name": "Pike", "dojo": "Eastleigh"},
          {"name": "Quinn", "dojo": "Ashford"},
          {"name": "Rowe", "dojo": "Brookvale"},
          {"name": "Sharpe", "dojo": "Ashford"},
          {"name": "Tate", "dojo": "Coalport"},
          {"name": "Usher", "dojo": "Brookvale"},
          {"name": "Vance", "dojo": "Coalport"},
          {"name": "Wren", "dojo": "Deerhurst"},
          {"name": "Yates", "dojo": "Eastleigh"}
        ],
        "poolsAfterDescent": [
          ["Abbott", "Hale", "Naylor", "Tate"],
          ["Barrow", "Grady", "Quinn", "Vance"],
          ["Dalton", "Keane", "Rowe", "Sharpe"],
          ["Ellis", "Mercer", "Usher", "Wren"],
          ["Chase", "Judd", "Lowry", "Pike"],
          ["Fenn", "Innes", "Oakes", "Yates"]
        ],
        "exchanges": [
          {"a": "Dalton", "b": "Yates"}
        ],
        "pools": [
          ["Abbott", "Hale", "Naylor", "Tate"],
          ["Barrow", "Grady", "Quinn", "Vance"],
          ["Yates", "Keane", "Rowe", "Sharpe"],
          ["Ellis", "Mercer", "Usher", "Wren"],
          ["Chase", "Judd", "Lowry", "Pike"],
          ["Fenn", "Innes", "Oakes", "Dalton"]
        ]
      }
    ]
  };
  /* BCDA-FIXTURE-END */

  var PLAY_MS = 1700;

  // ---------------------------------------------------------------- helpers

  function bitLen(n) {
    return n <= 0 ? 0 : 32 - Math.clz32(n);
  }

  // meetRound is the knockout round in which two slots can first meet: the
  // number of bits their slot numbers differ in. Neighbouring slots differ
  // only in the lowest bit and meet in round 1; slots in opposite halves
  // differ in the highest and meet in the final.
  function meetRound(i, j) {
    var d = i ^ j;
    return d <= 0 ? 0 : bitLen(d);
  }

  function el(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text !== undefined && text !== null) n.textContent = text;
    return n;
  }

  // ------------------------------------------------------------- the tree

  // buildTree mirrors the application's own tree: one leaf per knockout slot
  // a pool's winner can reach, and a node for every branch above them. A slot
  // no pool feeds is a bye and carries no capacity.
  function buildTree(winnerSlots, poolSizes, placed) {
    var poolAtSlot = {};
    var maxSlot = 0;
    var i;
    for (i = 0; i < winnerSlots.length; i++) {
      poolAtSlot[winnerSlots[i]] = i;
      if (winnerSlots[i] > maxSlot) maxSlot = winnerSlots[i];
    }
    var totalBits = bitLen(maxSlot);

    function rec(bitsLeft, prefix) {
      if (bitsLeft === 0) {
        var leaf = {
          poolIdx: -1, poolCount: 0, roomPools: 0, capacity: 0,
          dojoCount: Object.create(null), poolIndices: [], slot: prefix
        };
        if (Object.prototype.hasOwnProperty.call(poolAtSlot, prefix)) {
          var p = poolAtSlot[prefix];
          leaf.poolIdx = p;
          leaf.poolCount = 1;
          leaf.poolIndices = [p];
          var room = poolSizes[p] - placed[p];
          if (room > 0) {
            leaf.capacity = room;
            leaf.roomPools = 1;
          }
        }
        return leaf;
      }
      var l = rec(bitsLeft - 1, prefix << 1);
      var r = rec(bitsLeft - 1, (prefix << 1) | 1);
      return {
        left: l, right: r, poolIdx: -1, slot: -1,
        dojoCount: Object.create(null),
        capacity: l.capacity + r.capacity,
        poolCount: l.poolCount + r.poolCount,
        roomPools: l.roomPools + r.roomPools,
        poolIndices: l.poolIndices.concat(r.poolIndices)
      };
    }

    return { root: rec(totalBits, 0), totalBits: totalBits };
  }

  // record walks the slot's own path from the root and updates every node on
  // it, so a placement costs one walk rather than a re-count.
  function record(root, dojo, slot, totalBits, capacityDelta) {
    var node = root;
    var bitsLeft = totalBits;
    for (;;) {
      node.dojoCount[dojo] = (node.dojoCount[dojo] || 0) + 1;
      node.capacity += capacityDelta;
      if (bitsLeft === 0) break;
      bitsLeft--;
      node = ((slot >> bitsLeft) & 1) === 0 ? node.left : node.right;
    }
    if (capacityDelta < 0 && node.capacity === 0) {
      node = root;
      bitsLeft = totalBits;
      for (;;) {
        node.roomPools--;
        if (bitsLeft === 0) return;
        bitsLeft--;
        node = ((slot >> bitsLeft) & 1) === 0 ? node.left : node.right;
      }
    }
  }

  // worstMeeting is the best-effort crossing tie-break: the earliest round in
  // which a qualifier from any of these pools could meet a qualifier from a
  // pool this dojo already occupies. A larger number is a later meeting.
  function worstMeeting(candidatePools, winnerSlots, dojoPools) {
    var worst = Infinity;
    for (var a = 0; a < candidatePools.length; a++) {
      var p = candidatePools[a];
      if (p < 0 || p >= winnerSlots.length) continue;
      for (var b = 0; b < dojoPools.length; b++) {
        var q = dojoPools[b];
        if (q < 0 || q >= winnerSlots.length || q === p) continue;
        var r = meetRound(winnerSlots[p], winnerSlots[q]);
        if (r < worst) worst = r;
      }
    }
    return worst;
  }

  // ------------------------------------------------------- placement rules

  // emptiestPool is the rule used before a dojo has anyone on the sheet:
  // fewest of this dojo, then fewest players, then lowest pool.
  function emptiestPool(pools, poolSizes, dojo, counts) {
    var best = -1, bestDojo = 0, bestSize = 0;
    for (var i = 0; i < pools.length; i++) {
      if (pools[i].length >= poolSizes[i]) continue;
      var n = counts[i][dojo] || 0;
      if (best < 0 || n < bestDojo || (n === bestDojo && pools[i].length < bestSize)) {
        best = i;
        bestDojo = n;
        bestSize = pools[i].length;
      }
    }
    return best;
  }

  // branchLabel names a child for the caption. The last fork's children are
  // pools and are named outright; every fork above it splits a region of the
  // bracket, and the grid draws those regions stacked, so upper and lower
  // are what the reader sees.
  function branchLabel(node, kind, side, poolNames) {
    if (kind === "pool") {
      return node.poolIdx >= 0 ? poolNames[node.poolIdx] : "the bye";
    }
    return "the " + side + " " + kind;
  }

  function sentenceCase(s) {
    return s.charAt(0).toUpperCase() + s.slice(1);
  }

  // descend walks the tree for a new member of dojo and records, at every
  // fork, which branch won and why. The tiers are the application's own:
  // room first, then fewest of this dojo per pool, then more pools still
  // open, then more room per pool, then the latest possible meeting, then
  // the upper branch.
  function descend(tree, dojo, winnerSlots, dojoPools, poolNames) {
    var node = tree.root;
    var forks = [];
    var level = 0;

    while (node.left) {
      var l = node.left, r = node.right;
      var kind = !l.left ? "pool" : (level === 0 ? "half" : (level === 1 ? "quarter" : "branch"));
      var up = branchLabel(l, kind, "upper", poolNames);
      var lo = branchLabel(r, kind, "lower", poolNames);
      var lCount = l.dojoCount[dojo] || 0;
      var rCount = r.dojoCount[dojo] || 0;
      var chosen, why;

      if (l.capacity <= 0 && r.capacity <= 0) {
        return { poolIdx: -1, forks: forks };
      } else if (l.capacity <= 0) {
        chosen = "lower";
        why = sentenceCase(up) + " has no room left, so the draw goes to " + lo + ".";
      } else if (r.capacity <= 0) {
        chosen = "upper";
        why = sentenceCase(lo) + " has no room left, so the draw goes to " + up + ".";
      } else {
        var lc = lCount * r.poolCount;
        var rc = rCount * l.poolCount;
        var lcap = l.capacity * r.poolCount;
        var rcap = r.capacity * l.poolCount;
        if (lc !== rc) {
          chosen = lc < rc ? "upper" : "lower";
          // Branches of a bracket with an odd pool count hold different
          // numbers of pools, and the comparison is per pool, so two sides
          // holding 1 each are not level. Saying "fewer of the dojo" there
          // would read as a contradiction of the numbers just given.
          var evenly = l.poolCount === r.poolCount;
          why = holdings(up, lCount, l, lo, rCount, r, dojo) +
            (evenly ? " Fewer of the dojo wins, so the draw goes to " :
              " Fewer per pool wins, so the draw goes to ") +
            (chosen === "upper" ? up : lo) + ".";
        } else if (l.roomPools !== r.roomPools) {
          chosen = l.roomPools > r.roomPools ? "upper" : "lower";
          why = "Both " + kindPlural(kind) + " hold the same share of " + dojo + ". " +
            sentenceCase(up) + " has " + l.roomPools + " " + plural(l.roomPools, "pool") +
            " still open and " + lo + " has " + r.roomPools + ", so the draw goes to " +
            (chosen === "upper" ? up : lo) + ", leaving the rest of the dojo more pools to spread into.";
        } else if (lcap !== rcap) {
          chosen = lcap > rcap ? "upper" : "lower";
          why = "Level on the dojo and on open pools, so the draw goes to " +
            (chosen === "upper" ? up : lo) + ", which has more seats free.";
        } else {
          var lw = worstMeeting(l.poolIndices, winnerSlots, dojoPools);
          var rw = worstMeeting(r.poolIndices, winnerSlots, dojoPools);
          if (lw !== rw) {
            chosen = lw > rw ? "upper" : "lower";
            why = "Level on every count, so the draw goes to " + (chosen === "upper" ? up : lo) +
              ", whose qualifiers would meet " + dojo + " later in the knockout.";
          } else {
            chosen = "upper";
            why = kind === "pool"
              ? "Nothing separates " + up + " and " + lo + ", so the draw takes " + up + "."
              : "Nothing separates the two " + kindPlural(kind) + ", so the draw takes the upper one.";
          }
        }
      }

      forks.push({
        level: level,
        kind: kind,
        chosen: chosen,
        why: why,
        upperCount: lCount,
        lowerCount: rCount
      });
      node = chosen === "upper" ? l : r;
      level++;
    }

    if (node.poolIdx < 0 || node.capacity <= 0) return { poolIdx: -1, forks: forks };
    return { poolIdx: node.poolIdx, forks: forks };

    function holdings(upName, upN, upNode, loName, loN, loNode, d) {
      if (upNode.poolCount === loNode.poolCount) {
        return sentenceCase(upName) + " holds " + upN + " of " + d + " and " + loName +
          " holds " + loN + ".";
      }
      return sentenceCase(upName) + " holds " + upN + " of " + d + " across " +
        upNode.poolCount + " pools and " + loName + " holds " + loN + " across " +
        loNode.poolCount + " " + plural(loNode.poolCount, "pool") + ".";
    }
  }

  function plural(n, word) {
    return n === 1 ? word : word + "s";
  }

  // "halfs" is the plural an s would give and is not a word, so the branch
  // names carry their own.
  function kindPlural(kind) {
    return kind === "half" ? "halves" : kind + "s";
  }

  // run replays a whole preset and returns one step per competitor. Steps are
  // computed once, so stepping backwards is a re-render rather than a re-run.
  function run(preset) {
    var poolCount = preset.poolNames.length;
    var pools = [], counts = [], placed = [];
    var i, j;
    for (i = 0; i < poolCount; i++) {
      pools.push([]);
      counts.push(Object.create(null));
      placed.push(0);
    }
    var tree = buildTree(preset.winnerSlots, preset.poolSizes, placed);

    var footprint = Object.create(null);
    for (i = 0; i < preset.roster.length; i++) {
      var d = preset.roster[i].dojo;
      footprint[d] = (footprint[d] || 0) + 1;
    }
    function optimum(dojo) {
      return Math.ceil(footprint[dojo] / poolCount);
    }

    var steps = [];
    for (i = 0; i < preset.roster.length; i++) {
      var p = preset.roster[i];
      var forks = [], why = "", poolIdx, capNote = null;

      if (!tree.root.dojoCount[p.dojo]) {
        poolIdx = emptiestPool(pools, preset.poolSizes, p.dojo, counts);
        why = "Nobody from " + p.dojo + " is on the sheet yet, so there is no branch to avoid " +
          "and no tree to descend. " + p.name + " goes to the emptiest pool.";
      } else {
        var dojoPools = [];
        for (j = 0; j < poolCount; j++) {
          if (counts[j][p.dojo]) dojoPools.push(j);
        }
        var walk = descend(tree, p.dojo, preset.winnerSlots, dojoPools, preset.poolNames);
        forks = walk.forks;
        poolIdx = walk.poolIdx;
      }

      // A pool already holding this dojo's fair share is passed over while
      // any pool under that share still has room.
      if (poolIdx >= 0 && (counts[poolIdx][p.dojo] || 0) >= optimum(p.dojo)) {
        var masked = preset.poolSizes.slice();
        for (j = 0; j < poolCount; j++) {
          if ((counts[j][p.dojo] || 0) >= optimum(p.dojo)) masked[j] = pools[j].length;
        }
        var alt = emptiestPool(pools, masked, p.dojo, counts);
        if (alt >= 0) {
          capNote = preset.poolNames[poolIdx] + " already holds " + p.dojo + "'s share of " +
            optimum(p.dojo) + ", and another pool is under it, so " + p.name + " goes to " +
            preset.poolNames[alt] + " instead.";
          poolIdx = alt;
        }
      }

      if (poolIdx < 0) {
        throw new Error("no pool has room for " + p.name);
      }

      steps.push({
        kind: "place",
        index: i,
        name: p.name,
        dojo: p.dojo,
        poolIdx: poolIdx,
        slot: preset.winnerSlots[poolIdx],
        forks: forks,
        why: why,
        capNote: capNote
      });

      pools[poolIdx].push(p.name);
      counts[poolIdx][p.dojo] = (counts[poolIdx][p.dojo] || 0) + 1;
      placed[poolIdx]++;
      record(tree.root, p.dojo, preset.winnerSlots[poolIdx], tree.totalBits, -1);
    }

    var placedCount = steps.length;
    steps.push.apply(steps, exchangeSteps(preset, placedCount));
    return { steps: steps, totalBits: tree.totalBits, placedCount: placedCount };
  }

  // exchangeSteps turns the recorded exchanges into steps of their own. The
  // widget replays the descent, but it does not re-run the exchange pass that
  // follows it: those exchanges are recorded alongside the roster and pinned
  // against the application by a Go test, so what the board ends on is the
  // draw the application makes, not an approximation of it.
  function exchangeSteps(preset, placedCount) {
    var closing = "Every competitor is placed. The draw now looks at the finished sheet for " +
      "an exchange that would keep a dojo apart for longer.";
    if (!preset.exchanges.length) {
      return [{
        kind: "settled",
        index: placedCount,
        why: closing + " On this roster there is none to make, so the pools stand as drawn."
      }];
    }
    return preset.exchanges.map(function (ex, i) {
      return {
        kind: "exchange",
        index: placedCount + i,
        a: ex.a,
        b: ex.b,
        why: (i === 0 ? closing + " " : "") + ex.a + " and " + ex.b +
          " trade pools, which pushes their dojos' first meeting later in the knockout. " +
          "Exchanging is the last thing the draw does."
      };
    });
  }

  // ------------------------------------------------------------ rendering

  // Widget is a plain ES5 constructor (invoked with `new`, methods hung off
  // Widget.prototype below), not a Preact/React component -- this whole file
  // is vanilla DOM-manipulating JS with no React/Preact runtime in scope
  // (see the file header). oxlint's react plugin still flags `this` inside
  // any PascalCase-named function as a stateless-functional-component
  // mistake, which is the correct rule for actual React/Preact SFCs but a
  // false positive here: `this` is exactly right for a constructor's
  // per-instance state. The exemption lives in web-mobile/.oxlintrc.json's
  // `overrides` (scoped to docs/assets/javascripts/**), not an inline
  // disable/enable block here -- see that file for the rationale.
  function Widget(host) {
    this.host = host;
    this.presetIdx = 0;
    this.step = 0;
    this.timer = null;
    this.build();
    this.load(0);
  }

  Widget.prototype.build = function () {
    var self = this;
    this.host.innerHTML = "";
    this.host.classList.add("bcda");

    var head = el("div", "bcda__head");
    var tabs = el("div", "bcda__tabs");
    tabs.setAttribute("role", "group");
    tabs.setAttribute("aria-label", "Example roster");
    this.tabButtons = BCDA_FIXTURE.presets.map(function (preset, idx) {
      var b = el("button", "bcda__tab", preset.label);
      b.type = "button";
      b.addEventListener("click", function () { self.load(idx); });
      tabs.appendChild(b);
      return b;
    });
    head.appendChild(tabs);
    this.blurb = el("p", "bcda__blurb");
    head.appendChild(this.blurb);
    this.host.appendChild(head);

    this.rosterStrip = el("ol", "bcda__roster");
    this.rosterStrip.setAttribute("aria-label", "The entry list, in the order it arrives");
    this.host.appendChild(this.rosterStrip);

    var bar = el("div", "bcda__bar");
    this.backBtn = this.control(bar, "Back", "Place the previous competitor", function () {
      self.pause();
      self.goto(self.step - 1);
    });
    this.playBtn = this.control(bar, "Play", "Place the competitors one after another", function () {
      if (self.timer) {
        self.pause();
      } else {
        self.play();
      }
    });
    this.playBtn.classList.add("bcda__btn--primary");
    this.nextBtn = this.control(bar, "Step", "Place the next competitor", function () {
      self.pause();
      self.goto(self.step + 1);
    });
    this.resetBtn = this.control(bar, "Reset", "Start the draw again", function () {
      self.pause();
      self.goto(0);
    });
    this.progress = el("p", "bcda__progress");
    bar.appendChild(this.progress);
    this.host.appendChild(bar);

    this.grid = el("div", "bcda__grid");
    this.host.appendChild(this.grid);

    this.why = el("div", "bcda__why");
    this.why.setAttribute("aria-live", "polite");
    this.host.appendChild(this.why);

    this.legend = el("ul", "bcda__legend");
    this.legend.setAttribute("aria-label", "Dojos");
    this.host.appendChild(this.legend);
  };

  Widget.prototype.control = function (bar, label, title, fn) {
    var b = el("button", "bcda__btn", label);
    b.type = "button";
    b.title = title;
    b.addEventListener("click", fn);
    bar.appendChild(b);
    return b;
  };

  Widget.prototype.load = function (idx) {
    this.pause();
    this.presetIdx = idx;
    this.preset = BCDA_FIXTURE.presets[idx];
    this.result = run(this.preset);

    // Colours are keyed to the dojo's place in alphabetical order, not to the
    // order it first appears in the roster, so a dojo keeps the same colour
    // when the reader switches between the examples.
    var dojos = [];
    this.preset.roster.forEach(function (p) {
      if (dojos.indexOf(p.dojo) < 0) dojos.push(p.dojo);
    });
    this.dojos = dojos.sort();

    this.tabButtons.forEach(function (b, i) {
      b.classList.toggle("bcda__tab--on", i === idx);
      b.setAttribute("aria-pressed", i === idx ? "true" : "false");
    });
    this.blurb.textContent = this.preset.blurb;

    this.buildRoster();
    this.buildLegend();
    this.buildGrid();
    this.goto(0);
  };

  Widget.prototype.dojoIdx = function (dojo) {
    return this.dojos.indexOf(dojo);
  };

  Widget.prototype.buildRoster = function () {
    var self = this;
    this.rosterStrip.innerHTML = "";
    this.rosterCells = this.preset.roster.map(function (p) {
      var li = el("li", "bcda__entrant bcda__d" + self.dojoIdx(p.dojo));
      li.appendChild(el("span", "bcda__tag", p.dojo.charAt(0)));
      li.appendChild(el("span", "bcda__nm", p.name));
      li.title = p.name + ", " + p.dojo;
      self.rosterStrip.appendChild(li);
      return li;
    });
  };

  Widget.prototype.buildLegend = function () {
    var self = this;
    this.legend.innerHTML = "";
    this.dojos.forEach(function (dojo) {
      var li = el("li", "bcda__key bcda__d" + self.dojoIdx(dojo));
      li.appendChild(el("span", "bcda__tag", dojo.charAt(0)));
      li.appendChild(el("span", null, dojo));
      self.legend.appendChild(li);
    });
  };

  // buildGrid lays the tree out as columns: the whole bracket on the left,
  // then each split, then the pools themselves. One grid row per knockout
  // slot, so a branch is a cell spanning the rows beneath it.
  Widget.prototype.buildGrid = function () {
    var bits = this.result.totalBits;
    var leaves = 1 << bits;
    this.grid.innerHTML = "";
    // One narrow rail per branch level, then the pools take what is left.
    // minmax(0, 1fr) rather than 1fr so a long name cannot widen the track
    // and push the page sideways on a phone.
    this.grid.style.gridTemplateColumns =
      "repeat(" + bits + ", var(--bcda-rail)) minmax(0, 1fr)";
    this.grid.setAttribute("role", "group");
    this.grid.setAttribute("aria-label", "The knockout tree, with the pools that feed it");

    // Two headers, not one per level: the branch columns are only a rail wide,
    // and level names set over them wrap into unreadable fragments at any
    // width. The captions underneath name each level as the descent reaches
    // it, which is where the reader needs the word anyway.
    var treeHead = el("div", "bcda__colhead");
    treeHead.appendChild(el("span", "bcda__long", "Knockout tree"));
    treeHead.appendChild(el("span", "bcda__short", "Tree"));
    treeHead.style.gridColumn = "1 / span " + bits;
    treeHead.style.gridRow = "1";
    this.grid.appendChild(treeHead);

    var poolHead = el("div", "bcda__colhead bcda__colhead--left", "Pools");
    poolHead.style.gridColumn = String(bits + 1);
    poolHead.style.gridRow = "1";
    this.grid.appendChild(poolHead);

    this.nodeCells = {};
    for (var level = 0; level < bits; level++) {
      var span = leaves >> level;
      for (var prefix = 0; prefix < (1 << level); prefix++) {
        var cell = el("div", "bcda__node");
        cell.style.gridColumn = String(level + 1);
        cell.style.gridRow = (prefix * span + 2) + " / span " + span;
        cell.appendChild(el("span", "bcda__count", ""));
        this.grid.appendChild(cell);
        this.nodeCells[level + ":" + prefix] = cell;
      }
    }

    var poolAtSlot = {};
    this.preset.winnerSlots.forEach(function (s, i) { poolAtSlot[s] = i; });

    this.poolRows = [];
    this.poolSeats = [];
    for (var slot = 0; slot < leaves; slot++) {
      var row = el("div", "bcda__pool");
      row.style.gridColumn = String(bits + 1);
      row.style.gridRow = String(slot + 2);
      if (!Object.prototype.hasOwnProperty.call(poolAtSlot, slot)) {
        row.classList.add("bcda__pool--bye");
        row.appendChild(el("span", "bcda__poolname", "bye"));
        this.grid.appendChild(row);
        continue;
      }
      var idx = poolAtSlot[slot];
      row.appendChild(el("span", "bcda__poolname", this.preset.poolNames[idx]));
      var seats = el("div", "bcda__seats");
      var seatCells = [];
      for (var s = 0; s < this.preset.poolSizes[idx]; s++) {
        var seat = el("span", "bcda__seat");
        seats.appendChild(seat);
        seatCells.push(seat);
      }
      row.appendChild(seats);
      this.grid.appendChild(row);
      this.poolRows[idx] = row;
      this.poolSeats[idx] = seatCells;
    }
  };

  Widget.prototype.play = function () {
    var self = this;
    if (this.step >= this.result.steps.length) this.goto(0);
    this.playBtn.textContent = "Pause";
    this.timer = window.setInterval(function () {
      if (self.step >= self.result.steps.length) {
        self.pause();
        return;
      }
      self.goto(self.step + 1);
    }, PLAY_MS);
  };

  Widget.prototype.pause = function () {
    if (this.timer) {
      window.clearInterval(this.timer);
      this.timer = null;
    }
    if (this.playBtn) this.playBtn.textContent = "Play";
  };

  // goto renders the draw after n competitors have been placed. n === 0 is
  // the empty sheet; the final value shows the finished pools.
  // occupancyAfter replays the first n steps onto an empty sheet. Rebuilding
  // rather than undoing is what makes Back as cheap and as exact as Step.
  Widget.prototype.occupancyAfter = function (n) {
    var steps = this.result.steps;
    var seats = this.preset.poolNames.map(function () { return []; });
    for (var i = 0; i < n; i++) {
      var step = steps[i];
      if (step.kind === "place") {
        seats[step.poolIdx].push(step);
        continue;
      }
      if (step.kind !== "exchange") continue;
      var a = find(step.a), b = find(step.b);
      if (a && b) {
        var held = seats[a.pool][a.seat];
        seats[a.pool][a.seat] = seats[b.pool][b.seat];
        seats[b.pool][b.seat] = held;
      }
    }
    return seats;

    function find(name) {
      for (var p = 0; p < seats.length; p++) {
        for (var s = 0; s < seats[p].length; s++) {
          if (seats[p][s].name === name) return { pool: p, seat: s };
        }
      }
      return null;
    }
  };

  Widget.prototype.goto = function (n) {
    var self = this;
    var steps = this.result.steps;
    var placedCount = this.result.placedCount;
    n = Math.max(0, Math.min(n, steps.length));
    this.step = n;

    var current = n === 0 ? null : steps[n - 1];
    var occupants = this.occupancyAfter(n);
    var moved = current && current.kind === "exchange" ? [current.a, current.b] : [];

    this.poolSeats.forEach(function (seats, poolIdx) {
      seats.forEach(function (seat, s) {
        var who = occupants[poolIdx][s];
        seat.className = "bcda__seat";
        seat.textContent = "";
        seat.removeAttribute("title");
        if (!who) return;
        seat.classList.add("bcda__seat--full", "bcda__d" + self.dojoIdx(who.dojo));
        seat.appendChild(el("span", "bcda__tag", who.dojo.charAt(0)));
        seat.appendChild(el("span", "bcda__nm", who.name));
        seat.title = who.name + ", " + who.dojo;
        if (who === current || moved.indexOf(who.name) >= 0) {
          seat.classList.add("bcda__seat--new");
        }
      });
    });

    var placed = Math.min(n, placedCount);
    this.rosterCells.forEach(function (li, i) {
      li.classList.toggle("bcda__entrant--done", i < placed - 1 || n > placedCount);
      li.classList.toggle("bcda__entrant--now", n <= placedCount && i === placed - 1);
      li.classList.toggle("bcda__entrant--todo", i >= placed);
    });

    this.paintTree(current, occupants);
    this.paintWhy(current);
    this.progress.textContent = this.progressText(n, placed, placedCount);

    this.backBtn.disabled = n === 0;
    this.nextBtn.disabled = n === steps.length;
    this.resetBtn.disabled = n === 0;
    if (n === steps.length) this.pause();
  };

  Widget.prototype.progressText = function (n, placed, placedCount) {
    if (n === 0) return "Nobody placed yet, " + placedCount + " to go";
    if (n <= placedCount) return "Placed " + placed + " of " + placedCount;
    // Past the placements the readout follows the closing step's own kind,
    // not a count: a roster that needed no exchange has settled, and one that
    // needed exchanges says which of them is showing.
    if (this.result.steps[n - 1].kind === "settled") {
      return "All " + placedCount + " placed, and the draw settled";
    }
    return "All " + placedCount + " placed, exchange " + (n - placedCount) + " of " +
      (this.result.steps.length - placedCount);
  };

  Widget.prototype.paintTree = function (step, occupants) {
    var self = this;
    Object.keys(this.nodeCells).forEach(function (key) {
      var cell = self.nodeCells[key];
      cell.className = "bcda__node";
      cell.querySelector(".bcda__count").textContent = "";
      cell.removeAttribute("title");
    });
    this.poolRows.forEach(function (row) {
      if (row) row.classList.remove("bcda__pool--on", "bcda__pool--off");
    });
    if (!step) return;

    if (step.kind !== "place") {
      var involved = [];
      occupants.forEach(function (seats, poolIdx) {
        seats.forEach(function (who) {
          if (who && (who.name === step.a || who.name === step.b)) involved.push(poolIdx);
        });
      });
      if (involved.length) {
        this.poolRows.forEach(function (row, idx) {
          if (!row) return;
          row.classList.add(involved.indexOf(idx) >= 0 ? "bcda__pool--on" : "bcda__pool--off");
        });
      }
      return;
    }

    // A competitor whose dojo is not on the sheet yet never descends the tree,
    // so drawing a path for them would show a comparison that did not happen.
    // Only the pool they land in is marked.
    if (!step.forks.length) {
      this.poolRows.forEach(function (row, idx) {
        if (row) row.classList.add(idx === step.poolIdx ? "bcda__pool--on" : "bcda__pool--off");
      });
      return;
    }

    // The chosen slot's own bit path is the path the descent walked. Each cell
    // carries the count for ITS OWN branch, which is the number the fork above
    // it compared: fork L holds the counts of its two children, so those are
    // the two cells at level L + 1.
    var bits = this.result.totalBits;
    var label = function (cell, count, chosen) {
      if (!cell) return;
      cell.classList.add(chosen ? "bcda__node--on" : "bcda__node--off");
      cell.querySelector(".bcda__count").textContent = String(count);
      cell.title = "Holds " + count + " of " + step.dojo;
    };

    label(this.nodeCells["0:0"], step.forks[0].upperCount + step.forks[0].lowerCount, true);

    var prefix = 0;
    for (var level = 0; level + 1 < bits; level++) {
      var bit = (step.slot >> (bits - 1 - level)) & 1;
      var fork = step.forks[level];
      label(self.nodeCells[(level + 1) + ":" + (prefix << 1)], fork.upperCount, bit === 0);
      label(self.nodeCells[(level + 1) + ":" + ((prefix << 1) | 1)], fork.lowerCount, bit === 1);
      prefix = (prefix << 1) | bit;
    }

    this.poolRows.forEach(function (row, idx) {
      if (!row) return;
      row.classList.add(idx === step.poolIdx ? "bcda__pool--on" : "bcda__pool--off");
    });
  };

  Widget.prototype.paintWhy = function (step) {
    this.why.innerHTML = "";
    if (!step) {
      this.why.appendChild(el("p", "bcda__line",
        "The sheet is empty. Press Play to watch the draw fill it, or step through " +
        "one competitor at a time."));
      return;
    }

    if (step.kind !== "place") {
      var done = el("p", "bcda__now");
      done.appendChild(el("strong", null, "The draw is finished."));
      this.why.appendChild(done);
      this.why.appendChild(el("p", "bcda__line bcda__line--close", step.why));
      return;
    }

    var head = el("p", "bcda__now");
    head.appendChild(el("strong", null, step.name));
    head.appendChild(document.createTextNode(" of " + step.dojo + " goes to " +
      this.preset.poolNames[step.poolIdx] + "."));
    this.why.appendChild(head);

    if (step.why) this.why.appendChild(el("p", "bcda__line", step.why));
    step.forks.forEach(function (f) {
      var line = el("p", "bcda__line");
      line.appendChild(el("span", "bcda__step", String(f.level + 1)));
      line.appendChild(document.createTextNode(f.why));
      this.why.appendChild(line);
    }, this);
    if (step.capNote) this.why.appendChild(el("p", "bcda__line", step.capNote));
  };

  // ---------------------------------------------------------------- mount

  var mounted = [];

  function mount() {
    // Material's instant navigation replaces the page body rather than
    // reloading, so a widget left playing on the page we came from would keep
    // its timer running against nodes no longer in the document. Retire those
    // before mounting whatever this page brought.
    mounted = mounted.filter(function (widget) {
      if (widget.host.isConnected) return true;
      widget.pause();
      return false;
    });

    var hosts = document.querySelectorAll("[data-pool-draw-animation]");
    Array.prototype.forEach.call(hosts, function (host) {
      if (host.dataset.bcdaMounted === "1") return;
      host.dataset.bcdaMounted = "1";
      var placeholder = host.innerHTML;
      try {
        mounted.push(new Widget(host));
      } catch (e) {
        // Put the page's own placeholder back rather than leaving an empty
        // box on the page; the prose around it already carries the meaning.
        host.dataset.bcdaMounted = "";
        host.classList.remove("bcda");
        host.innerHTML = placeholder;
        console.error("pool-draw-animation failed to mount:", e);
      }
    });
  }

  if (window.document$ && typeof window.document$.subscribe === "function") {
    window.document$.subscribe(mount);
  } else {
    document.addEventListener("DOMContentLoaded", mount);
  }
})();
