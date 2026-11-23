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
            self.result["headline_image_url"] = normalize_wiki_url(meta_tag.get("content"))

    def extract_nearby_routes(self):
        # New HTML: section#nearby > p.inline-links > a
        nearby_section = self.soup.find("section", id="nearby")
        if nearby_section:
            links = nearby_section.find_all("a", href=True)
            nearby = []
            for link in links:
                href = link["href"]
                ref = href.split("/")[-1]
                nearby.append(
                    {
                        "description": sanitize_text(link.text),
                        "objectID": get_object_id(ref),
                        "ref": ref,
                    }
                )
            if nearby:
                self.result["nearby"] = nearby
        else:
            # Fallback: old HTML structure with h3.panel-title
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
        # New HTML: section#download > p > a (with GPX file link)
        download_section = self.soup.find("section", id="download")
        if download_section:
            link = download_section.find("a", href=True)
            if link:
                self._process_gpx_link(link["href"])
            return

        # Fallback: old HTML structure with h2.gpsfiles
        header = self.soup.find("h2", class_="gpsfiles", string="GPX File")
        if header:
            link = header.find_next("a", href=True)
            if link:
                url = f"http://gps-routes.co.uk{link['href']}"
                self._process_gpx_link(url)

    def _process_gpx_link(self, url: str):
        """Download GPX, extract coordinates, and store GitHub URL."""
        if not url.startswith("http"):
            url = f"https://www.gps-routes.co.uk{url}"
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
        image_details = []

        # New HTML: div.photo-item > a > img + div.photo-caption
        photo_items = self.soup.find_all("div", class_="photo-item")
        if photo_items:
            for item in photo_items:
                image_link = item.find("a", href=True)
                image_img = item.find("img", src=True)
                caption_div = item.find("div", class_="photo-caption")
                if image_link and image_img:
                    image_details.append(
                        {
                            "src": normalize_wiki_url(image_img["src"]),
                            "title": sanitize_text(image_link.get("title", "No title")),
                            "caption": sanitize_text(caption_div.text) if caption_div else sanitize_text(image_img.get("alt", "")),
                        }
                    )
        else:
            # Fallback: old HTML structure with div.thumbnail
            image_containers = self.soup.find_all("div", class_="thumbnail")
            for container in image_containers:
                image_link = container.find("a", href=True)
                image_img = container.find("img", src=True)
                caption = container.find_next_sibling("div", class_="caption")
                if image_link and image_img and caption:
                    image_details.append(
                        {
                            "src": normalize_wiki_url(image_img["src"]),
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
        # New HTML: span with distance text under h1 in section#details
        # e.g. "25 miles (40.2 km)"
        details_section = self.soup.find("section", id="details")
        if details_section:
            dist_span = details_section.find("span")
            if dist_span:
                dist_text = dist_span.text
                match = re.search(r"(\d+\.?\d*)\s*km", dist_text)
                if match:
                    self.result["distance_km"] = float(match.group(1))
                    return

        # Fallback: old HTML structure with div.dist
        dist_div = self.soup.find("div", class_="dist")
        if dist_div:
            dist_text = dist_div.text
            match = re.search(r"\((\d+\.?\d*)\s*km\)", dist_text)
            if match:
                self.result["distance_km"] = float(match.group(1))

    def extract_description(self):
        # New HTML: p#desc-text inside section#details
        # The description may contain .dots and .more-text spans for the More/Less toggle
        desc_paragraph = self.soup.find("p", id="desc-text")
        if desc_paragraph:
            # Extract full text including the hidden .more-text part
            parts = []
            for element in desc_paragraph.children:
                if isinstance(element, str):
                    parts.append(element)
                elif element.name == "br":
                    parts.append(" ")
                elif element.name == "span" and element.get("class") and "more-text" in element.get("class", []):
                    parts.append(element.get_text(strip=True))
                elif element.name != "span" or (element.get("class") and "dots" in element.get("class", [])):
                    # Skip .dots span ("...")
                    if not (element.name == "span" and element.get("class") and "dots" in element.get("class", [])):
                        parts.append(element.get_text(strip=True))
            text = "".join(parts)
            self.result["description"] = sanitize_text(text)
            return

        # Fallback: old HTML structure with div#topmaindiv
        panel_body = self.soup.find("div", attrs={"id": "topmaindiv"})
        if panel_body:
            paragraph = panel_body.find("p")
            if paragraph:
                self.result["description"] = sanitize_text(
                    paragraph.get_text(separator=" ", strip=True)
                )

    def extract_details(self):
        sections = []
        # New HTML: look for h3.subheaderroute or h2.subheaderroute with following p.para
        for heading in self.soup.find_all(["h2", "h3"], class_="subheaderroute"):
            paragraph = heading.find_next_sibling("p", class_="para")
            if paragraph:
                sections.append(
                    {
                        "subtitle": sanitize_text(heading.text),
                        "content": sanitize_text(paragraph.text),
                    }
                )

        self.result["details"] = sections


def normalize_wiki_url(url: str) -> str:
    """Normalize Wikipedia thumbnail sizes to 500px standard."""
    return url.replace("512px-", "500px-").replace("256px-", "500px-")


def sanitize_text(text: str) -> str:
    return (
        text.replace(" , ", ", ")
        .replace(" . ", ". ")
        .replace("\n", " ")
        .replace("\\ ", " ")
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
