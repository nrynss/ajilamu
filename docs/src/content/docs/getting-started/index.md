---
title: Introduction
description: The origins of Ajilamu and the core philosophy of rhythmic dubbing.
template: doc
---

Ajilamu dubs creator video into another language while preserving timing and rhythm. It holds every translated line inside the slot the original speaker left for it.

---

## Where the Name Comes From

Aji Lhamu is the masked dance drama of the Monpa people in western Arunachal Pradesh. The tradition spans roughly six centuries. Actors perform the drama across five days using dance, song, and pantomime.

The play carries the Tibetan telling of the Ramayana. That story crossed multiple languages and mountain passes before reaching the Monpa valleys. The performers carry the tale across one more cultural boundary.

The theatrical tradition descends from Thangtong Gyalpo. He lived in the fifteenth century as an engineer, poet, and dramatist. Gyalpo staged musical performances across Tibet to finance iron suspension bridges over torrential Himalayan rivers. Mukto Monpa tradition credits the dance with funding 108 iron chain bridges.

---

## The Dubbing Problem

Two historical elements inspire this software.

First, a masked player cannot rely on facial expression. The mask fixes the actor's face in wood and paint. The voice must land on bodily movement with exact timing. That is the core dubbing challenge, staged six hundred years early.

Second, the theatre funded crossings. Dramatic art moved a story across linguistic borders. The proceeds built physical bridges that carried people across chasms. Software that transports spoken stories across languages shares that heritage.

---

## The Architectural Approach

Modern video dubbing often creates floating speech. Translations run too long or finish too fast. Listeners detect unnatural pauses and desynchronized lip movements.

Ajilamu attacks that friction with three engineering decisions:

1. **Hard Duration Constraints**: Gemini translates each line under an exact time budget. The prompt treats slot duration as a physical limit rather than a loose guideline.
2. **Two-Sided Fit Measurement**: Underruns break immersion as sharply as overruns. The system measures the signed time delta for every synthesized take.
3. **Discrete Takes on Disk**: Every synthesis attempt writes a discrete WAV file. The engine preserves background room tone and original audio beds.

---

## Next Steps

- Proceed to the **[Quickstart](/ajilamu/getting-started/quickstart/)** to set up Ajilamu on your local machine.
- Explore the **[Pipeline Architecture](/ajilamu/architecture/pipeline/)** to understand the translation and synthesis stages.
