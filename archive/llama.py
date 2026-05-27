import json
import requests
from tqdm import tqdm

from utils import walk_files


# Define the prompt template
system_prompt = """
You are a structured data extractor. Given a document, output ONLY valid JSON — no text before or after.

Extract the following from the document. Use generic descriptions, not specific names from the text.
Use singular array items ("pub/cafe" not "pubs/cafes"). Only include facilities explicitly mentioned.
If a value cannot be determined, use JSON null.

{
  "estimated_duration": "time-based estimate e.g. '1 hour', 'half day'",
  "difficulty": "easy | moderate | hard",
  "terrain": ["e.g. woodland, coastal, mountain"],
  "points_of_interest": ["e.g. historical site, viewpoint"],
  "facilities": ["e.g. pub/cafe, car park, dog friendly, play area"],
  "route_type": "circular | one-way | out and back",
  "activities": ["e.g. walking, cycling, horse riding"]
}

Rules:
- Use consistent terminology across all outputs
- Include "dog friendly" in facilities if dog walking is mentioned
- To estimate duration, consider the activity type and distance: assume ~3km/h walking, ~15km/h cycling.
"""


def get_facets(document: str) -> tuple[dict, float]:

    # Define the API endpoint
    url = "http://hydra.local:8080/v1/chat/completions"

    payload = {
        "messages": [
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": document},
        ],
        "temperature": 0.0,
        "top_p": 1.0,
        "top_k": 1,
        "n_predict": 256,
        "response_format": {"type": "json_object"},
    }

    response = requests.post(url, json=payload, timeout=(120, 180))
    response.raise_for_status()

    data = response.json()
    # print(json.dumps(data, indent=2))

    content = data["choices"][0]["message"]["content"]
    tokens_per_second = data["timings"]["predicted_per_second"]

    return json.loads(content), tokens_per_second


def further_details(details: list[dict]) -> str:
    return "\n\n".join(
        [
            f"{detail['subtitle']}\n{detail['content']}\n\n"
            for detail in details
            if detail["subtitle"] != "Further Information and Other Local Ideas"
        ]
    )


for file in tqdm(
    list(walk_files("../data/backup")), desc="Summarizing facets", unit="record"
):
    with open(file, "r") as fp:
        record: dict = json.load(fp)

        if record.get("llama_cpp", False):
            continue

        document = f"""
{record["title"]} ({record["distance_km"]} km)
{record.get("display_address", "")}

{record["description"]}

{further_details(record["details"])}
        """

        try:
            facets, tokens_per_second = get_facets(document)
            tqdm.write(f"{file}: {tokens_per_second:.1f} t/s")
        except (requests.RequestException, json.JSONDecodeError, KeyError) as e:
            tqdm.write(f"Failed {file}: {e}")
            continue

        record["llama_cpp"] = True
        record.update(facets)

        with open(file, "w") as fp:
            json.dump(record, fp, indent=2)
