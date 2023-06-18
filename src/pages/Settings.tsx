import React, { useState, useEffect, ChangeEvent } from "react";
import moment from "moment-timezone";
import Button from "../components/Button";
import Select from "../components/Select";
import FileInput from "../components/FileInput";
import InputNumber from "../components/InputNumber";
import ToggleSwitch from "../components/ToggleSwitch";

const Settings: React.FC = () => {
  const [isSSL, setIsSSL] = useState(false);
  const [port, setPort] = useState(0);
  const [maxVideos, setMaxVideos] = useState(0);
  const [maxSizePerVideo, setMaxSizePerVideo] = useState(0);
  const [timeZone, setTimeZone] = useState("UTC");
  const [dateTimeFormat, setDateTimeFormat] = useState("YYYY/MM/DD HH:mm:ss");

  const dateTimeFormats = ["YYYY/MM/DD HH:mm:ss", "DD-MM-YYYY HH:mm:ss", "MM/DD/YYYY hh:mm:ss A"];
  const timeZones = moment.tz.names();

  const importSettings = async () => {
    const response = await fetch("http://localhost:3000/api/manage/import");
    const settings = await response.json();
    setIsSSL(settings.isSSL);
    setPort(settings.port);
    setMaxVideos(settings.maxVideos);
    setMaxSizePerVideo(settings.maxSizePerVideo);
    setTimeZone(settings.timeZone);
    setDateTimeFormat(settings.dateTimeFormat);
  };

  useEffect(() => {
    importSettings();
  }, []);

  const handleSSLToggle = () => {
    setIsSSL(!isSSL);
  };

  const handleDeleteVideos = async () => {
    // TODO
    if (window.confirm("Are you sure?")) {
      await fetch("http://localhost:3000/api/manage/delete/videos", {
        method: "DELETE",
      });
    }
  };

  const exportSettings = async () => {
    const settings = {
      isSSL,
      port,
      maxVideos,
      maxSizePerVideo,
      timeZone,
      dateTimeFormat,
    };

    await fetch("http://localhost:3000/api/manage/export", {
      // TODO
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(settings),
    });
  };

  const handlePortChange = (event: ChangeEvent<HTMLInputElement>) => {
    setPort(Number(event.target.value));
  };

  const handleMaxVideosChange = (event: ChangeEvent<HTMLInputElement>) => {
    setMaxVideos(Number(event.target.value));
  };

  const handleMaxSizePerVideoChange = (event: ChangeEvent<HTMLInputElement>) => {
    setMaxSizePerVideo(Number(event.target.value));
  };

  const handleTimeZoneChange = (event: ChangeEvent<HTMLSelectElement>) => {
    setTimeZone(event.target.value);
  };

  const handleDateTimeFormatChange = (event: ChangeEvent<HTMLSelectElement>) => {
    setDateTimeFormat(event.target.value);
  };

  return (
    <div className="p-4 sm:ml-64">
      <div className="p-4 mt-14">
        <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">Paramètres</h1>
        <div className="mb-6">
          <p className="dark:text-stone-100 mb-2">Supprimer toutes les vidéos</p>
          <Button text="Supprimer" onClick={handleDeleteVideos} />
        </div>
        <div className="mb-6">
          <Select
            label="Time Zone"
            id="timeZone"
            value={timeZone}
            onChange={handleTimeZoneChange}
            options={timeZones}
          />
        </div>
        <div className="mb-6">
          <Select
            label="DateTime Format"
            id="dateTimeFormat"
            value={dateTimeFormat}
            onChange={handleDateTimeFormatChange}
            options={dateTimeFormats}
          />
        </div>
        <div className="mb-6">
          <InputNumber label="Port" id="port" value={port} onChange={handlePortChange} required />
        </div>
        <div className="mb-6">
          <InputNumber
            label="Maximum number of videos"
            id="maxVideos"
            value={maxVideos}
            onChange={handleMaxVideosChange}
            required
          />
        </div>
        <div className="mb-6">
          <InputNumber
            label="Maximum size per video"
            id="maxSizePerVideo"
            value={maxSizePerVideo}
            onChange={handleMaxSizePerVideoChange}
            required
          />
        </div>
        <div className="mb-6">
          <ToggleSwitch label="Enable SSL" id="ssl" checked={isSSL} onChange={handleSSLToggle} />
        </div>
        <div className="mb-6">
          <FileInput label="Import settings" id="importSettings" onChange={importSettings} />
        </div>
        <Button text="Export settings" onClick={exportSettings} />
      </div>
    </div>
  );
};

export default Settings;
