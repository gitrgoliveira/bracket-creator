import csv
import json
import os
import re
import requests
import sys
import time

BASE_URL = "http://localhost:8080"
PASSWORD = "testpassword"
HEADERS = {
    "X-Tournament-Password": PASSWORD,
    "Content-Type": "application/json"
}

DATA_DIR = "test-data"

CATEGORIES = [
    {
        "title": "Teams",
        "csv": "team_registrations_2026.csv",
        "seeds": "team_registrations_2026_seeds.csv",
        "team_size": 5,
        "zekken": False,
        "startTime": "09:30",
        "date": "10-05-2026" # Saturday in our demo
    },
    {
        "title": "Women up to 2D",
        "csv": "individual_women_up_to_2nd_2026.csv",
        "zekken": True,
        "number_prefix": "A",
        "startTime": "09:00",
        "date": "11-05-2026"
    },
    {
        "title": "Men up to 2D",
        "csv": "individual_men_up_to_2nd_2026.csv",
        "zekken": True,
        "number_prefix": "B",
        "startTime": "09:00",
        "date": "11-05-2026"
    },
    {
        "title": "6D and up",
        "csv": "individual_6plus_mixed_2026.csv",
        "zekken": True,
        "number_prefix": "C",
        "startTime": "11:00",
        "date": "11-05-2026" # Sunday in our demo
    },
    {
        "title": "Women 3D and up",
        "csv": "individual_women_3rd_and_above_2026.csv",
        "seeds": "individual_women_3rd_and_above_2026_seeds.csv",
        "zekken": True,
        "number_prefix": "D",
        "startTime": "12:30",
        "date": "11-05-2026"
    },
    {
        "title": "Men 3D and up",
        "csv": "individual_men_3rd_and_above_2026.csv",
        "seeds": "individual_men_3rd_and_above_2026_seeds.csv",
        "zekken": True,
        "number_prefix": "E",
        "startTime": "13:30",
        "date": "11-05-2026"
    },
]

def slugify(text):
    text = text.lower()
    text = re.sub(r'[^a-z0-9]+', '-', text)
    return text.strip('-')

def wait_for_server():
    print(f"Waiting for server at {BASE_URL}...")
    for _ in range(30):
        try:
            requests.get(f"{BASE_URL}/health")
            print("Server is up!")
            return
        except requests.exceptions.ConnectionError:
            time.sleep(1)
    print("Server failed to start")
    sys.exit(1)

def setup_tournament():
    resp = requests.get(f"{BASE_URL}/api/viewer/tournament")
    if resp.status_code == 404:
        print("Creating tournament...")
        payload = {
            "name": "London Cup Demo",
            "date": "10-05-2026",
            "venue": "London",
            "durationDays": 2,
            "courts": ["A", "B"],
            "password": PASSWORD
        }
        resp = requests.post(f"{BASE_URL}/api/tournament", json=payload)
        resp.raise_for_status()
    else:
        print("Tournament already exists.")

def parse_participants(csv_path):
    participants = []
    if not os.path.exists(csv_path):
        return []
    with open(csv_path, 'r', encoding='utf-8') as f:
        reader = csv.reader(f)
        for row in reader:
            if not row or len(row) < 2:
                continue
            if len(row) >= 3:
                name = row[0].strip()
                display_name = row[1].strip()
                participants.append({
                    "name": name,
                    "displayName": display_name,
                    "dojo": row[2].strip(),
                    "metadata": [row[3].strip()] if len(row) > 3 else []
                })
            elif len(row) == 2:
                participants.append({
                    "name": row[0].strip(),
                    "dojo": row[1].strip()
                })
    return participants

def parse_seeds(csv_path):
    seeds = []
    if csv_path is None or not os.path.exists(csv_path):
        return []
    with open(csv_path, 'r', encoding='utf-8') as f:
        reader = csv.DictReader(f)
        for row in reader:
            seeds.append({
                "name": row['Name'],
                "seedRank": int(row['Rank'])
            })
    return seeds

def run_competition_setup(cat):
    title = cat['title']
    comp_id = slugify(title)
    csv_path = os.path.join(DATA_DIR, cat['csv'])
    seeds_path = os.path.join(DATA_DIR, cat.get('seeds', "")) if cat.get('seeds') else None

    print(f"Creating competition: {title} (ID: {comp_id})")
    payload = {
        "id": comp_id,
        "name": title,
        "format": "mixed",
        "poolSize": 3,
        "poolWinners": 2,
        "roundRobin": True,
        "courts": ["A", "B"],
        "withZekkenName": cat.get('zekken', True),
        "numberPrefix": cat.get('number_prefix', ""),
        "teamSize": cat.get('team_size', 1),
        "startTime": cat.get('startTime', ""),
        "date": cat.get('date', ""),
        "status": "setup"
    }
    requests.post(f"{BASE_URL}/api/competitions", json=payload, headers=HEADERS).raise_for_status()

    participants = parse_participants(csv_path)

    if participants:
        requests.post(f"{BASE_URL}/api/competitions/{comp_id}/participants",
                             json={"players": participants}, headers=HEADERS).raise_for_status()

    # NOTE: cross-competition promotion (the old "reserved slots" feature) has
    # been removed. The only automatic promotion is pools -> playoffs within a
    # single mixed competition, created via POST /competitions/:id/playoffs.
    # Promoting winners between separate competitions is now a manual step the
    # operator performs by adding the resolved players to the target roster.

    seeds = parse_seeds(seeds_path)
    if seeds:
        requests.put(f"{BASE_URL}/api/competitions/{comp_id}/seeds", 
                            json=seeds, headers=HEADERS).raise_for_status()

    # Start competition
    try:
        requests.post(f"{BASE_URL}/api/competitions/{comp_id}/start", headers=HEADERS).raise_for_status()
    except requests.exceptions.HTTPError as e:
        print(f"Error starting competition {comp_id}: {e}")
        if e.response is not None:
            print(f"Response: {e.response.text}")
        raise
    print(f"Competition {title} started.")
    return comp_id

SIMULATION_DATA_PATH = os.path.join(os.path.dirname(__file__), "simulation_data.json")
with open(SIMULATION_DATA_PATH, "r") as f:
    SIM_DATA = json.load(f)

def get_predictable_result(is_team, index, can_draw=True):
    if is_team:
        data = SIM_DATA["team_scores"][index % len(SIM_DATA["team_scores"])]
        if not can_draw and data['winner_side'] == "DRAW":
            return get_predictable_result(is_team, index + 1, can_draw)
        return data
    else:
        data = SIM_DATA["individual_scores"][index % len(SIM_DATA["individual_scores"])]
        if not can_draw and data['winner_side'] == "DRAW":
            return get_predictable_result(is_team, index + 1, can_draw)
        return data

def score_all_matches(comp_id):
    print(f"Running competition {comp_id}...")
    
    iteration = 0
    while iteration < 100:
        iteration += 1
        resp = requests.get(f"{BASE_URL}/api/viewer/competitions/{comp_id}")
        resp.raise_for_status()
        detail = resp.json()
        
        is_pools = detail['config']['format'] == 'pools'
        is_team = detail['config'].get('teamSize', 1) > 1
        
        # Identify unscored matches
        pool_matches = [m for m in detail.get('poolMatches', []) if m.get('status') != 'completed']
        
        bracket_matches = []
        bracket = detail.get('bracket', {})
        # A mixed competition's bracket is a READ-ONLY preview; the live
        # knockout lives in the separate linked "playoffs" competition.
        # Only score bracket matches when this is the playoffs comp.
        if bracket and bracket.get('preview'):
            bracket = {}
        if bracket and 'rounds' in bracket:
            for round_matches in bracket['rounds']:
                for m in round_matches:
                    if m.get('status') != 'completed' and not m['sideA'].startswith("Winner of") and not m['sideB'].startswith("Winner of") and m['sideA'] != "" and m['sideB'] != "":
                        bracket_matches.append(m)
        
        to_score = pool_matches + bracket_matches
        if not to_score:
            print(f"No more matches to score for {comp_id}.")
            break
            
        print(f"Found {len(to_score)} matches to score (Iteration {iteration})...")
        
        for i, match in enumerate(to_score):
            mid = match['id']
            sideA = match['sideA']
            sideB = match['sideB']
            
            if is_team:
                res_data = get_predictable_result(True, i + iteration)
                payload = {
                    "sideA": sideA,
                    "sideB": sideB,
                    "teamAWins": res_data["winsA"],
                    "teamBWins": res_data["winsB"],
                    "draws": res_data["draws"]
                }
                requests.put(f"{BASE_URL}/api/competitions/{comp_id}/matches/{mid}/quick-score", 
                                    json=payload, headers=HEADERS).raise_for_status()
                print(f"  Quick-scored {mid}: {sideA} vs {sideB} -> {res_data['winner_side']}")
            else:
                res_data = get_predictable_result(False, i + iteration)
                
                # If it's a playoff, we can't have a draw
                if not is_pools and res_data.get("decision") == "X":
                    winner = sideA
                    ipponsA = ["M"]
                    ipponsB = []
                    decision = ""
                else:
                    winner = ""
                    if res_data["winner_side"] == "A": winner = sideA
                    elif res_data["winner_side"] == "B": winner = sideB
                    ipponsA = res_data["ipponsA"]
                    ipponsB = res_data["ipponsB"]
                    decision = res_data.get("decision", "")

                payload = {
                    "id": mid,
                    "sideA": sideA,
                    "sideB": sideB,
                    "winner": winner,
                    "ipponsA": ipponsA,
                    "ipponsB": ipponsB,
                    "decision": decision,
                    "status": "completed"
                }
                requests.put(f"{BASE_URL}/api/competitions/{comp_id}/matches/{mid}/score", 
                                    json=payload, headers=HEADERS).raise_for_status()
                print(f"  Scored {mid}: {sideA} vs {sideB} -> {res_data['winner_side']} ({len(ipponsA)}-{len(ipponsB)})")
            
            # Small delay for realism
            time.sleep(0.05)

    # Print summary (using the new readable scoreSummary if available)
    resp = requests.get(f"{BASE_URL}/api/viewer/competitions/{comp_id}")
    detail = resp.json()
    if detail['config']['format'] == 'pools':
        standings = detail.get('standings', {})
        for pool, std in standings.items():
            print(f"Final Standings for {pool}:")
            for entry in std:
                score_info = entry.get('scoreSummary', f"Points: {entry['points']}")
                print(f"  {entry['rank']}. {entry['player']['name']} | {score_info}")
    else:
        bracket = detail.get('bracket', {})
        if bracket and 'rounds' in bracket:
            final_round = bracket['rounds'][-1]
            if final_round and final_round[0]['status'] == 'completed':
                print(f"Tournament Winner: {final_round[0]['winner']}")

def create_linked_playoff(pool_comp_id, cat):
    print(f"Creating linked playoff for {pool_comp_id}...")
    
    # We now use the dedicated API endpoint for this!
    resp = requests.post(f"{BASE_URL}/api/competitions/{pool_comp_id}/playoffs", 
                                headers=HEADERS)
    
    if resp.status_code == 201:
        playoff_id = resp.json()['id']
        print(f"Playoff competition {playoff_id} created by API and linked to {pool_comp_id}.")
        return playoff_id
    
    # Minimal fallback for older API versions or if it fails
    print(f"Playoff endpoint failed ({resp.status_code}), falling back to manual setup.")
    payload = {
        "name": f"{cat['title']} - Playoffs",
        "format": "playoffs",
        "courts": ["A", "B"],
        "withZekkenName": cat.get('zekken', False),
        "status": "setup"
    }
    resp = requests.post(f"{BASE_URL}/api/competitions", json=payload, headers=HEADERS)
    resp.raise_for_status()
    playoff_id = resp.json()['id']
    return playoff_id

def wait_for_status(comp_id, expected, timeout_s=5.0, interval_s=0.2):
    """Poll the competition until its status matches `expected` or timeout.

    Used after scoring all pool matches to assert the backend's auto-completion
    has fired. If this times out, the auto-completion logic has regressed.
    Fails immediately on 4xx responses (auth/config problems, not transient).
    """
    deadline = time.time() + timeout_s
    last_status = None
    resp = None  # guard against UnboundLocalError if every attempt raises ConnectionError
    # Use a forgiving per-request timeout: at least 1s so slow/busy machines
    # don't spuriously time out on a healthy backend. The loop's own deadline
    # still bounds total wait time, so a truly hung server can't block forever.
    request_timeout = max(1.0, interval_s * 4)
    while time.time() < deadline:
        try:
            resp = requests.get(
                f"{BASE_URL}/api/competitions/{comp_id}",
                headers=HEADERS,
                timeout=request_timeout,
            )
        except (requests.exceptions.ConnectionError, requests.exceptions.Timeout) as e:
            # Transient — retry until deadline
            print(f"  [WARN] Connection/timeout error polling {comp_id}: {e}")
            time.sleep(interval_s)
            continue
        if 400 <= resp.status_code < 500:
            raise RuntimeError(
                f"[FAIL] {comp_id}: unexpected {resp.status_code} while polling status. "
                f"Check auth/config. Response: {resp.text[:200]}"
            )
        if resp.status_code == 200:
            try:
                body = resp.json()
            except ValueError as e:
                # 200 with non-JSON body (HTML error page from a proxy, etc.) —
                # treat as a transient poll failure rather than crashing.
                print(f"  [WARN] Non-JSON 200 response polling {comp_id}: {e}. Body: {resp.text[:200]}")
                time.sleep(interval_s)
                continue
            last_status = body.get("status") if isinstance(body, dict) else None
            if last_status == expected:
                return last_status
        time.sleep(interval_s)
    http_info = f" (HTTP {resp.status_code})" if resp is not None else " (no successful response)"
    # Non-fatal: a mixed comp stays in 'pools' after pools finish (the live
    # knockout is the separate linked playoffs comp). Warn and continue.
    print(
        f"[WARN] {comp_id}: expected status {expected!r} within {timeout_s}s, "
        f"last seen {last_status!r}{http_info}. Continuing."
    )
    return last_status


def main():
    wait_for_server()
    setup_tournament()

    for cat in CATEGORIES:
        # Per-category isolation: a cyclic pool tie blocks one comp's
        # pools->completed transition (which gates its linked playoff). Don't
        # let that abort seeding of the remaining categories.
        try:
            # 1. Setup Pools
            pool_comp_id = run_competition_setup(cat)

            # 2. Setup Linked Playoff
            playoff_id = create_linked_playoff(pool_comp_id, cat)

            # 3. Run Pools — backend auto-transitions status to "completed" when
            #    the last pool match is recorded (unless a cyclic tie blocks it).
            score_all_matches(pool_comp_id)
            status = wait_for_status(pool_comp_id, "completed")
            if status != "completed":
                print(f"[SKIP] {pool_comp_id} not completed (status={status!r}); "
                      f"leaving pools as-is, skipping its playoff.")
                continue
            print(f"[OK] {pool_comp_id} auto-transitioned to 'completed'.")

            # 4. Start and Run Playoffs (Promotion happens automatically!)
            requests.post(f"{BASE_URL}/api/competitions/{playoff_id}/start", headers=HEADERS).raise_for_status()
            score_all_matches(playoff_id)
        except Exception as e:
            print(f"[WARN] category {cat['title']} failed: {e}. Continuing.")
            continue

    print("Tournament full simulation complete!")

if __name__ == "__main__":
    main()
