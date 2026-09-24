import type { StoryContext } from "@storybook/react-vite";
import { useState } from "react";
import { UPDATE_STORY_ARGS } from "storybook/internal/core-events";
import { addons } from "storybook/preview-api";

export function useStoryArg<Args extends object, Key extends keyof Args>(
	args: Args,
	key: Key,
	context: Pick<StoryContext, "id">,
): [Args[Key], (next: Args[Key]) => void] {
	const [value, setValue] = useState<Args[Key]>(() => args[key]);
	const [followed, setFollowed] = useState<Args[Key]>(() => args[key]);

	if (!Object.is(args[key], followed)) {
		setFollowed(() => args[key]);
		setValue(() => args[key]);
	}

	return [
		value,
		(next) => {
			setValue(() => next);
			addons.getChannel().emit(UPDATE_STORY_ARGS, {
				storyId: context.id,
				updatedArgs: { [key]: next },
			});
		},
	];
}
