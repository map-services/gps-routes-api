import asyncio
from enum import unique
import functools
import json
import random
from bs4 import BeautifulSoup
import requests

from detail_extractor import DetailExtractor
from utils import get_datahub_client, ROUTES_INDEX
from algoliasearch.http.exceptions import RequestException


datahub = get_datahub_client()


def extract_next_link(soup: BeautifulSoup) -> str | None:
    next_text = soup.find(string="Next")
    if next_text:
        parent_a_tag = next_text.find_parent("a", href=True)
        if parent_a_tag:
            return parent_a_tag.get("href")

    return None


def extract_links(soup: BeautifulSoup) -> list[str]:
    results = []
    target_div = soup.find(
        "div",
        align="center",
        string=lambda text: text and "RoutesLinksWalks" in text,
    )
    if target_div:
        target_table = target_div.find_next("table")
        if target_table:
            td_elements = target_table.find_all("td")
            for td in td_elements:
                links = td.find_all("a")
                for link in links:
                    href = link.get("href")
                    if href:
                        results.append(href)

    return results


async def main():

    hrefs = []
    base_url = "https://www.gps-routes.co.uk"
    path = "/A55CD9/home.nsf/RoutesLinksWalks?OpenView&Start=1"
    prev = None

    while path != prev:
        print(f"checking path {path}, current={len(hrefs)}")

        markup = requests.get(f"{base_url}{path}").text
        soup = BeautifulSoup(markup, "lxml")

        prev = path
        path = extract_next_link(soup)
        hrefs.extend(extract_links(soup))

    with open("../data/full-list.txt", "w") as fp:
        for href in sorted(set(hrefs)):
            fp.write(href + "\n")


asyncio.run(main())
