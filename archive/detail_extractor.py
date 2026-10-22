import asyncio
from datetime import datetime, timezone
import json
import re
from bs4 import BeautifulSoup
import hashlib
import gpxpy
import requests

from utils import get_object_id

GITHUB_GPX_BASE = "https://raw.githubusercontent.com/rm-hull/gps-routes-api/refs/heads/main/data/routes"


class DetailExtractor:
    def __init__(self, markup: str):
        self.soup = BeautifulSoup(markup, "lxml")
        self.result = {}

    def process(self):
        self.extract_metadata()
        self.extract_nearby_routes()
        self.extract_gpx_link()
        self.extract_photos()
        self.extract_distance_km()
        self.extract_description()
        self.extract_details()
        self.extract_video_link()
        return self.result

    def extract_metadata(self):
        self.result["created_at"] = datetime.now(timezone.utc).isoformat()

        meta_tag = self.soup.find("meta", property="og:url")
        if meta_tag:
            ref = meta_tag.get("content").split("/")[-1].split("?")[0]
            self.result["objectID"] = get_object_id(ref)
            self.result["ref"] = ref

        meta_tag = self.soup.find("meta", property="og:title")
        if meta_tag:
            self.result["title"] = meta_tag.get("content")

        meta_tag = self.soup.find("meta", property="og:image")
        if meta_tag:
            self.result["headline_image_url"] = meta_tag.get("content")

    def extract_nearby_routes(self):
        h3_title = self.soup.find(
            "h3", class_="panel-title", string="Cycle Routes and Walking Routes Nearby"
        )

        if h3_title:
            panel_body = h3_title.find_parent(
                "div", class_="panel-heading"
            ).find_next_sibling("div", class_="panel-body")

            if panel_body:
                links = panel_body.find_all("a")
                route_links = [(link.text, link["href"]) for link in links]
                nearby = []
                for text, href in route_links:
                    ref = href.split("/")[-1]
                    nearby.append(
                        {
                            "description": sanitize_text(text),
                            "objectID": get_object_id(ref),
                            "ref": ref,
                        }
                    )

                self.result["nearby"] = nearby

    def extract_gpx_link(self):
        header = self.soup.find("h2", class_="gpsfiles", string="GPX File")
        if header:
            link = header.find_next("a", href=True)
            if link:
                url = f"http://gps-routes.co.uk{link['href']}"

                try:
                    object_id = self.result["objectID"]
                    payload = requests.get(url).text
                    with open(f"../data/routes/{object_id[0]}/{object_id}.gpx", "w") as fp:
                        fp.write(payload)

                    # Store the GitHub raw URL instead of the original site URL
                    self.result["gpx_url"] = f"{GITHUB_GPX_BASE}/{object_id[0]}/{object_id}.gpx"

                    gpx = gpxpy.parse(payload)
                    if gpx.routes:
                        self.result["_geoloc"] = {
                            "lat": gpx.routes[0].points[0].latitude,
                            "lng": gpx.routes[0].points[0].longitude,
                        }
                    elif gpx.tracks:
                        self.result["_geoloc"] = {
                            "lat": gpx.tracks[0].segments[0].points[0].latitude,
                            "lng": gpx.tracks[0].segments[0].points[0].longitude,
                        }
                    else:
                        print(f"No routes found in GPX file: {gpx}")

                except Exception as e:
                    print("Failed to process URL: ", e)
                    # Fall back to the original URL if GPX processing fails
                    self.result["gpx_url"] = url

    def extract_photos(self):
        image_containers = self.soup.find_all("div", class_="thumbnail")
        image_details = []

        for container in image_containers:
            image_link = container.find("a", href=True)
            image_img = container.find("img", src=True)
            caption = container.find_next_sibling("div", class_="caption")
            if image_link and image_img and caption:
                image_details.append(
                    {
                        "src": image_img["src"],
                        "title": sanitize_text(image_link.get("title", "No title")),
                        "caption": sanitize_text(caption.text),
                    }
                )

        if image_details:
            self.result["images"] = image_details

    def extract_video_link(self):
        iframe = self.soup.find("iframe", id="video")

        if iframe and "src" in iframe.attrs:
            self.result["video_url"] = iframe["src"]

    def extract_distance_km(self):
        dist_div = self.soup.find("div", class_="dist")

        if dist_div:
            dist_text = dist_div.text
            match = re.search(r"\((\d+\.?\d*)\s*km\)", dist_text)
            if match:
                self.result["distance_km"] = float(match.group(1))

    def extract_description(self):
        panel_body = self.soup.find("div", attrs={"id": "topmaindiv"})
        if panel_body:
            paragraph = panel_body.find("p")
            if paragraph:
                self.result["description"] = sanitize_text(
                    paragraph.get_text(separator=" ", strip=True)
                )

    def extract_details(self):
        sections = []
        for h2 in self.soup.find_all("h2", class_="subheaderroute"):
            paragraph = h2.find_next_sibling("p", class_="para")
            if paragraph:
                sections.append(
                    {
                        "subtitle": sanitize_text(h2.text),
                        "content": sanitize_text(paragraph.text),
                    }
                )

        self.result["details"] = sections


def sanitize_text(text: str) -> str:
    return (
        text.replace(" , ", ", ")
        .replace(" . ", ". ")
        .replace("\n", " ")
        .replace("\ ", " ")
        .replace(" ", " ")
        .replace("  ", " ")
        .strip()
    )


async def main():

    # Fetch sample page
    # url = "https://www.gps-routes.co.uk/routes/home.nsf/RoutesLinksWalks/deils-cauldron-walking-route"
    # markup = requests.get(url).text
    # soup = BeautifulSoup(markup, "lxml")

    test_file = "data/examples/deils-cauldron-walking-route.html"
    # test_file = "data/examples/aber-falls-walking-route.html"

    with open(test_file, "r", encoding="utf-8", errors="replace") as fp:
        result = DetailExtractor(fp.read().replace("\uFFFD", " ")).process()
        print(json.dumps(result, indent=2, sort_keys=True))


if __name__ == "__main__":
    asyncio.run(main())
