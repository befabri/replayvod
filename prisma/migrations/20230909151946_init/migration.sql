-- CreateTable
CREATE TABLE `AppAccessToken` (
    `id` INTEGER NOT NULL AUTO_INCREMENT,
    `accessToken` VARCHAR(191) NOT NULL,
    `expiresAt` DATETIME(3) NOT NULL,

    UNIQUE INDEX `AppAccessToken_accessToken_key`(`accessToken`),
    PRIMARY KEY (`id`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `FetchLog` (
    `fetchId` VARCHAR(191) NOT NULL,
    `userId` VARCHAR(191) NOT NULL,
    `fetchedAt` DATETIME(3) NOT NULL,
    `fetchType` VARCHAR(191) NOT NULL,

    PRIMARY KEY (`fetchId`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `EventSub` (
    `id` INTEGER NOT NULL AUTO_INCREMENT,
    `userId` VARCHAR(191) NOT NULL,
    `fetchId` VARCHAR(191) NOT NULL,

    PRIMARY KEY (`id`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `Subscription` (
    `id` VARCHAR(191) NOT NULL,
    `status` VARCHAR(191) NOT NULL,
    `subscriptionType` VARCHAR(191) NOT NULL,
    `broadcasterId` VARCHAR(191) NOT NULL,
    `createdAt` DATETIME(3) NOT NULL,
    `cost` INTEGER NOT NULL,

    PRIMARY KEY (`id`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `SubscriptionEventSub` (
    `eventSubId` INTEGER NOT NULL,
    `subscriptionId` VARCHAR(191) NOT NULL,

    PRIMARY KEY (`eventSubId`, `subscriptionId`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `Event` (
    `id` INTEGER NOT NULL AUTO_INCREMENT,
    `externalEventId` VARCHAR(191) NULL,
    `broadcasterId` VARCHAR(191) NOT NULL,
    `eventType` VARCHAR(191) NOT NULL,
    `startedAt` DATETIME(3) NOT NULL,
    `endAt` DATETIME(3) NULL,
    `subscriptionId` VARCHAR(191) NOT NULL,

    PRIMARY KEY (`id`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `User` (
    `userId` VARCHAR(191) NOT NULL,
    `fetchedAt` DATETIME(3) NOT NULL,

    PRIMARY KEY (`userId`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `Channel` (
    `broadcasterId` VARCHAR(191) NOT NULL,
    `broadcasterLogin` VARCHAR(191) NOT NULL,
    `broadcasterName` VARCHAR(191) NOT NULL,
    `displayName` VARCHAR(191) NOT NULL,
    `broadcasterType` VARCHAR(191) NOT NULL,
    `createdAt` DATETIME(3) NOT NULL,
    `description` VARCHAR(191) NOT NULL,
    `offlineImageUrl` VARCHAR(191) NOT NULL,
    `profileImageUrl` VARCHAR(191) NOT NULL,
    `profilePicture` VARCHAR(191) NOT NULL,
    `type` VARCHAR(191) NULL,
    `viewCount` INTEGER NOT NULL DEFAULT 0,

    UNIQUE INDEX `Channel_broadcasterLogin_key`(`broadcasterLogin`),
    PRIMARY KEY (`broadcasterId`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `UserFollowedChannels` (
    `id` INTEGER NOT NULL AUTO_INCREMENT,
    `userId` VARCHAR(191) NOT NULL,
    `broadcasterId` VARCHAR(191) NOT NULL,
    `followedAt` DATETIME(3) NOT NULL,

    PRIMARY KEY (`id`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `Job` (
    `id` VARCHAR(191) NOT NULL,
    `status` ENUM('PENDING', 'RUNNING', 'DONE', 'FAILED') NOT NULL,

    PRIMARY KEY (`id`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `Log` (
    `id` INTEGER NOT NULL AUTO_INCREMENT,
    `downloadUrl` VARCHAR(191) NOT NULL,
    `filename` VARCHAR(191) NOT NULL,
    `lastWriteTime` DATETIME(3) NOT NULL,
    `type` VARCHAR(191) NOT NULL,

    PRIMARY KEY (`id`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `Task` (
    `id` VARCHAR(191) NOT NULL,
    `name` VARCHAR(191) NOT NULL,
    `description` VARCHAR(191) NOT NULL,
    `taskType` VARCHAR(191) NOT NULL,
    `interval` INTEGER NOT NULL DEFAULT 0,
    `lastDuration` INTEGER NOT NULL DEFAULT 0,
    `lastExecution` DATETIME(3) NOT NULL,
    `nextExecution` DATETIME(3) NOT NULL,
    `metadata` VARCHAR(191) NULL,

    PRIMARY KEY (`id`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `Video` (
    `id` VARCHAR(191) NOT NULL,
    `filename` VARCHAR(191) NOT NULL,
    `status` ENUM('PENDING', 'RUNNING', 'DONE', 'FAILED') NOT NULL,
    `displayName` VARCHAR(191) NOT NULL,
    `broadcasterId` VARCHAR(191) NOT NULL,
    `startDownloadAt` DATETIME(3) NOT NULL,
    `downloadedAt` DATETIME(3) NOT NULL,
    `jobId` VARCHAR(191) NOT NULL,
    `viewerCount` INTEGER NOT NULL DEFAULT 0,
    `language` VARCHAR(191) NOT NULL,
    `quality` ENUM('LOW', 'MEDIUM', 'HIGH') NOT NULL,
    `duration` DOUBLE NOT NULL,
    `size` DOUBLE NOT NULL,
    `thumbnail` VARCHAR(191) NOT NULL,

    UNIQUE INDEX `Video_filename_key`(`filename`),
    UNIQUE INDEX `Video_jobId_key`(`jobId`),
    PRIMARY KEY (`id`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `VideoRequest` (
    `videoId` VARCHAR(191) NOT NULL,
    `userId` VARCHAR(191) NOT NULL,

    PRIMARY KEY (`videoId`, `userId`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `Category` (
    `id` VARCHAR(191) NOT NULL,
    `boxArtUrl` VARCHAR(191) NOT NULL,
    `igdbId` VARCHAR(191) NULL,
    `name` VARCHAR(191) NOT NULL,

    UNIQUE INDEX `Category_igdbId_key`(`igdbId`),
    UNIQUE INDEX `Category_name_key`(`name`),
    PRIMARY KEY (`id`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `VideoCategory` (
    `videoId` VARCHAR(191) NOT NULL,
    `categoryId` VARCHAR(191) NOT NULL,

    PRIMARY KEY (`videoId`, `categoryId`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `StreamCategory` (
    `streamId` VARCHAR(191) NOT NULL,
    `categoryId` VARCHAR(191) NOT NULL,

    PRIMARY KEY (`streamId`, `categoryId`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `DownloadScheduleCategory` (
    `downloadScheduleId` INTEGER NOT NULL,
    `categoryId` VARCHAR(191) NOT NULL,

    PRIMARY KEY (`downloadScheduleId`, `categoryId`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `Tag` (
    `name` VARCHAR(191) NOT NULL,

    PRIMARY KEY (`name`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `VideoTag` (
    `videoId` VARCHAR(191) NOT NULL,
    `tagId` VARCHAR(191) NOT NULL,

    PRIMARY KEY (`videoId`, `tagId`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `StreamTag` (
    `streamId` VARCHAR(191) NOT NULL,
    `tagId` VARCHAR(191) NOT NULL,

    PRIMARY KEY (`streamId`, `tagId`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `DownloadScheduleTag` (
    `downloadScheduleId` INTEGER NOT NULL,
    `tagId` VARCHAR(191) NOT NULL,

    PRIMARY KEY (`downloadScheduleId`, `tagId`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `Stream` (
    `id` VARCHAR(191) NOT NULL,
    `fetchId` VARCHAR(191) NOT NULL,
    `fetchedAt` DATETIME(3) NOT NULL,
    `isMature` BOOLEAN NOT NULL,
    `language` VARCHAR(191) NOT NULL,
    `startedAt` DATETIME(3) NOT NULL,
    `thumbnailUrl` VARCHAR(191) NOT NULL,
    `title` VARCHAR(191) NOT NULL,
    `type` VARCHAR(191) NOT NULL,
    `broadcasterId` VARCHAR(191) NOT NULL,
    `viewerCount` INTEGER NOT NULL DEFAULT 0,

    PRIMARY KEY (`id`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `Title` (
    `id` INTEGER NOT NULL AUTO_INCREMENT,
    `name` VARCHAR(191) NOT NULL,

    PRIMARY KEY (`id`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `VideoTitle` (
    `videoId` VARCHAR(191) NOT NULL,
    `titleId` INTEGER NOT NULL,

    PRIMARY KEY (`videoId`, `titleId`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- CreateTable
CREATE TABLE `DownloadSchedule` (
    `id` INTEGER NOT NULL AUTO_INCREMENT,
    `provider` ENUM('SINGLE_CHANNEL', 'FOLLOWED_CHANNEL') NOT NULL,
    `broadcasterId` VARCHAR(191) NULL,
    `viewersCount` INTEGER NULL,
    `timeBeforeDelete` DATETIME(3) NULL,
    `trigger` ENUM('CATEGORY', 'TAG', 'MINIMUM_VIEW', 'ONLINE') NOT NULL,
    `quality` ENUM('LOW', 'MEDIUM', 'HIGH') NOT NULL,
    `isDeleteRediff` BOOLEAN NOT NULL,
    `requestedBy` VARCHAR(191) NOT NULL,

    PRIMARY KEY (`id`)
) DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- AddForeignKey
ALTER TABLE `FetchLog` ADD CONSTRAINT `FetchLog_userId_fkey` FOREIGN KEY (`userId`) REFERENCES `User`(`userId`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `EventSub` ADD CONSTRAINT `EventSub_userId_fkey` FOREIGN KEY (`userId`) REFERENCES `User`(`userId`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `EventSub` ADD CONSTRAINT `EventSub_fetchId_fkey` FOREIGN KEY (`fetchId`) REFERENCES `FetchLog`(`fetchId`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `Subscription` ADD CONSTRAINT `Subscription_broadcasterId_fkey` FOREIGN KEY (`broadcasterId`) REFERENCES `Channel`(`broadcasterId`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `SubscriptionEventSub` ADD CONSTRAINT `SubscriptionEventSub_eventSubId_fkey` FOREIGN KEY (`eventSubId`) REFERENCES `EventSub`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `SubscriptionEventSub` ADD CONSTRAINT `SubscriptionEventSub_subscriptionId_fkey` FOREIGN KEY (`subscriptionId`) REFERENCES `Subscription`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `Event` ADD CONSTRAINT `Event_broadcasterId_fkey` FOREIGN KEY (`broadcasterId`) REFERENCES `Channel`(`broadcasterId`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `Event` ADD CONSTRAINT `Event_subscriptionId_fkey` FOREIGN KEY (`subscriptionId`) REFERENCES `Subscription`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `UserFollowedChannels` ADD CONSTRAINT `UserFollowedChannels_userId_fkey` FOREIGN KEY (`userId`) REFERENCES `User`(`userId`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `UserFollowedChannels` ADD CONSTRAINT `UserFollowedChannels_broadcasterId_fkey` FOREIGN KEY (`broadcasterId`) REFERENCES `Channel`(`broadcasterId`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `Video` ADD CONSTRAINT `Video_broadcasterId_fkey` FOREIGN KEY (`broadcasterId`) REFERENCES `Channel`(`broadcasterId`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `Video` ADD CONSTRAINT `Video_jobId_fkey` FOREIGN KEY (`jobId`) REFERENCES `Job`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `Video` ADD CONSTRAINT `Video_id_fkey` FOREIGN KEY (`id`) REFERENCES `Stream`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `VideoRequest` ADD CONSTRAINT `VideoRequest_videoId_fkey` FOREIGN KEY (`videoId`) REFERENCES `Video`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `VideoRequest` ADD CONSTRAINT `VideoRequest_userId_fkey` FOREIGN KEY (`userId`) REFERENCES `User`(`userId`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `VideoCategory` ADD CONSTRAINT `VideoCategory_videoId_fkey` FOREIGN KEY (`videoId`) REFERENCES `Video`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `VideoCategory` ADD CONSTRAINT `VideoCategory_categoryId_fkey` FOREIGN KEY (`categoryId`) REFERENCES `Category`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `StreamCategory` ADD CONSTRAINT `StreamCategory_streamId_fkey` FOREIGN KEY (`streamId`) REFERENCES `Stream`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `StreamCategory` ADD CONSTRAINT `StreamCategory_categoryId_fkey` FOREIGN KEY (`categoryId`) REFERENCES `Category`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `DownloadScheduleCategory` ADD CONSTRAINT `DownloadScheduleCategory_downloadScheduleId_fkey` FOREIGN KEY (`downloadScheduleId`) REFERENCES `DownloadSchedule`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `DownloadScheduleCategory` ADD CONSTRAINT `DownloadScheduleCategory_categoryId_fkey` FOREIGN KEY (`categoryId`) REFERENCES `Category`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `VideoTag` ADD CONSTRAINT `VideoTag_videoId_fkey` FOREIGN KEY (`videoId`) REFERENCES `Video`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `VideoTag` ADD CONSTRAINT `VideoTag_tagId_fkey` FOREIGN KEY (`tagId`) REFERENCES `Tag`(`name`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `StreamTag` ADD CONSTRAINT `StreamTag_streamId_fkey` FOREIGN KEY (`streamId`) REFERENCES `Stream`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `StreamTag` ADD CONSTRAINT `StreamTag_tagId_fkey` FOREIGN KEY (`tagId`) REFERENCES `Tag`(`name`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `DownloadScheduleTag` ADD CONSTRAINT `DownloadScheduleTag_downloadScheduleId_fkey` FOREIGN KEY (`downloadScheduleId`) REFERENCES `DownloadSchedule`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `DownloadScheduleTag` ADD CONSTRAINT `DownloadScheduleTag_tagId_fkey` FOREIGN KEY (`tagId`) REFERENCES `Tag`(`name`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `Stream` ADD CONSTRAINT `Stream_broadcasterId_fkey` FOREIGN KEY (`broadcasterId`) REFERENCES `Channel`(`broadcasterId`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `VideoTitle` ADD CONSTRAINT `VideoTitle_videoId_fkey` FOREIGN KEY (`videoId`) REFERENCES `Video`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `VideoTitle` ADD CONSTRAINT `VideoTitle_titleId_fkey` FOREIGN KEY (`titleId`) REFERENCES `Title`(`id`) ON DELETE RESTRICT ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `DownloadSchedule` ADD CONSTRAINT `DownloadSchedule_broadcasterId_fkey` FOREIGN KEY (`broadcasterId`) REFERENCES `Channel`(`broadcasterId`) ON DELETE SET NULL ON UPDATE CASCADE;

-- AddForeignKey
ALTER TABLE `DownloadSchedule` ADD CONSTRAINT `DownloadSchedule_requestedBy_fkey` FOREIGN KEY (`requestedBy`) REFERENCES `User`(`userId`) ON DELETE RESTRICT ON UPDATE CASCADE;
