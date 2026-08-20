import { useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import L from "leaflet";
import "leaflet/dist/leaflet.css";
import markerIcon from "leaflet/dist/images/marker-icon.png";
import markerIcon2x from "leaflet/dist/images/marker-icon-2x.png";
import markerShadow from "leaflet/dist/images/marker-shadow.png";
import { api } from "../lib/api";
import { Spinner, formatFee } from "../components/ui";

// Vite doesn't resolve Leaflet's default icon URLs; point them at the bundled
// assets so markers render.
const icon = L.icon({
  iconUrl: markerIcon,
  iconRetinaUrl: markerIcon2x,
  shadowUrl: markerShadow,
  iconSize: [25, 41],
  iconAnchor: [12, 41],
  popupAnchor: [1, -34],
  shadowSize: [41, 41],
});

export function MapView() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { data, isLoading } = useQuery({ queryKey: ["facilities", {}], queryFn: () => api.listFacilities() });
  const mapEl = useRef<HTMLDivElement>(null);
  const mapRef = useRef<L.Map | null>(null);

  useEffect(() => {
    if (!mapEl.current || !data) return;
    const placed = data.filter((f) => f.latitude !== 0 || f.longitude !== 0);
    if (placed.length === 0) return;

    if (!mapRef.current) {
      mapRef.current = L.map(mapEl.current);
      L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
        attribution: "&copy; OpenStreetMap contributors",
        maxZoom: 19,
      }).addTo(mapRef.current);
    }
    const map = mapRef.current;

    const markers = placed.map((f) => {
      const m = L.marker([f.latitude, f.longitude], { icon }).addTo(map);
      m.bindPopup(
        `<strong>${f.name}</strong><br/>${f.location}<br/>${formatFee(f.feeCents)}<br/>` +
          `<a href="#" data-id="${f.id}" class="fb-popup-link">${t("map.view")}</a>`,
      );
      m.on("popupopen", (e) => {
        const link = e.popup.getElement()?.querySelector<HTMLAnchorElement>(".fb-popup-link");
        link?.addEventListener("click", (ev) => {
          ev.preventDefault();
          navigate(`/facilities/${f.id}`);
        });
      });
      return m;
    });

    map.fitBounds(L.featureGroup(markers).getBounds().pad(0.3));
    return () => {
      markers.forEach((m) => m.remove());
    };
  }, [data, navigate, t]);

  // Tear the map down on unmount.
  useEffect(() => () => { mapRef.current?.remove(); mapRef.current = null; }, []);

  if (isLoading) return <Spinner />;

  return (
    <div>
      <h2 className="mb-1 text-2xl font-semibold">{t("map.title")}</h2>
      <p className="mb-6 text-slate-500">{t("map.subtitle")}</p>
      <div ref={mapEl} className="h-[70vh] w-full overflow-hidden rounded-xl border border-slate-200" />
    </div>
  );
}
